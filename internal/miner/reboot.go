package miner

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type RebootResult struct{ State, Message string }

func rebootFailure(message string) RebootResult { return RebootResult{"failed", message} }
func rebootResponse(response btcHTTPReply, err error) RebootResult {
	if err != nil {
		return RebootResult{"unknown", "请求已尝试发送，但连接中断或超时；结果未确认，不自动重试"}
	}
	if response.code >= 200 && response.code < 300 {
		if m, e := decode(response.body); e == nil {
			if success, ok := m["success"].(bool); ok && !success {
				return rebootFailure("设备返回重启失败")
			}
			if text(m, "error") != "" {
				return rebootFailure("设备返回重启错误")
			}
			if code := number(m, "code"); code != nil && *code != 0 {
				return rebootFailure("设备返回非成功状态码")
			}
		}
		return RebootResult{"accepted", "设备已响应重启请求，尚未确认重启完成"}
	}
	return rebootFailure(fmt.Sprintf("设备拒绝重启请求（HTTP %d）", response.code))
}

// Reboot is only called by the explicit selected-device action, never by polling.
func Reboot(ctx context.Context, ip, brand, model, protocol string, creds Credentials) RebootResult {
	address := net.ParseIP(ip)
	if address == nil || address.To4() == nil {
		return rebootFailure("无效IPv4地址")
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	base := "http://" + ip
	c := newBTCHTTP(base, nil)
	defer c.client.CloseIdleConnections()
	switch {
	case strings.Contains(protocol, "REST"):
		return rebootREST(ctx, c, creds)
	case brand == "Avalon":
		return rebootAvalon(ctx, c, model)
	case brand == "Antminer":
		return rebootAntminer(ctx, c, creds)
	case brand == "Whatsminer":
		// Only this explicitly selected LAN miner's HTTPS client accepts its local,
		// self-signed control-panel certificate. Redirects remain disabled.
		if !address.IsPrivate() && !address.IsLoopback() {
			return rebootFailure("神马网页重启仅支持局域网IP")
		}
		c.base = "https://" + ip
		if tr, ok := c.client.Transport.(*http.Transport); ok {
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}
		}
		return rebootWhatsminerWeb(ctx, c, creds)
	default:
		return rebootFailure("未知品牌，未发送重启命令")
	}
}
func rebootAntminer(ctx context.Context, c *btcHTTPClient, creds Credentials) RebootResult {
	path := "/cgi-bin/reboot.cgi"
	// First request may itself reboot an authentication-free endpoint; retry only
	// on an explicit 401 challenge, never after a timeout or successful response.
	response, e := c.request(ctx, path, "GET", "")
	if e != nil || response.code != 401 {
		return rebootResponse(response, e)
	}
	auth, e := digestHeader(response.header.Get("WWW-Authenticate"), creds, "GET", path)
	if e != nil {
		return rebootFailure("蚂蚁后台Digest认证失败")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if e != nil {
		return rebootFailure("无法创建重启请求")
	}
	req.Header.Set("Authorization", auth)
	resp, e := c.client.Do(req)
	if e != nil {
		return rebootResponse(btcHTTPReply{}, e)
	}
	defer resp.Body.Close()
	return rebootResponse(btcHTTPReply{code: resp.StatusCode}, nil)
}

var rebootFormToken = regexp.MustCompile(`token\s*:\s*['"]([^'"\r\n]{1,512})['"]`)

func rebootWhatsminerWeb(ctx context.Context, c *btcHTTPClient, creds Credentials) RebootResult {
	login, e := c.request(ctx, "/cgi-bin/luci", "POST", url.Values{"luci_username": {creds.Username}, "luci_password": {creds.Password}}.Encode())
	if e != nil || login.code != 302 || len(login.header.Values("Set-Cookie")) == 0 {
		return rebootFailure("神马后台登录失败；请检查账号密码或固件接口")
	}
	page, e := c.request(ctx, "/cgi-bin/luci/admin/system/reboot", "GET", "")
	if e != nil || page.code != 200 {
		return rebootFailure("神马重启页面读取失败")
	}
	match := rebootFormToken.FindSubmatch(page.body)
	if len(match) < 2 {
		return rebootFailure("神马后台未返回重启令牌")
	}
	response, e := c.request(ctx, "/cgi-bin/luci/admin/system/reboot/call", "POST", url.Values{"token": {string(match[1])}}.Encode())
	return rebootResponse(response, e)
}
func rebootREST(ctx context.Context, c *btcHTTPClient, creds Credentials) RebootResult {
	info, _, _ := restJSON(ctx, c.client, c.base, "/api/v1/info", "GET", "", nil)
	m, _ := decode(info)
	family := firmwareFamily(m)
	if family == "" {
		family = identifyRESTPage(ctx, c.client, c.base, nil)
	}
	if family == "" {
		return rebootFailure("未确认倍腾/兼容固件身份，未发送重启")
	}
	payload, _ := json.Marshal(map[string]string{"pw": creds.Password})
	unlocked, _, e := restJSON(ctx, c.client, c.base, "/api/v1/unlock", "POST", "", payload)
	if e != nil {
		return rebootFailure("固件解锁失败")
	}
	m, e = decode(unlocked)
	token := text(m, "token")
	if e != nil || token == "" || len(token) > 8192 {
		return rebootFailure("固件未返回有效令牌")
	}
	req, e := http.NewRequestWithContext(ctx, "POST", c.base+"/api/v1/system/reboot", strings.NewReader("{}"))
	if e != nil {
		return rebootFailure("无法创建重启请求")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response, e := c.client.Do(req)
	if e != nil {
		return rebootResponse(btcHTTPReply{}, e)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil || len(body) > maxResponse {
		return RebootResult{"unknown", "设备响应未完整读取，结果未确认，不自动重试"}
	}
	return rebootResponse(btcHTTPReply{code: response.StatusCode, body: body}, nil)
}

func rebootAvalon(ctx context.Context, c *btcHTTPClient, model string) RebootResult {
	family := "AvalonDevice"
	if !strings.Contains(strings.ToLower(model), "1566") {
		root, e := c.request(ctx, "/", "GET", "")
		if e != nil {
			return rebootFailure("无法识别阿瓦隆后台")
		}
		family = btcHTTPFamily(root)
	}
	if family == "AvalonLuci" {
		login, e := c.request(ctx, "/cgi-bin/luci/avalon/page/index", "POST", url.Values{"username": {"root"}, "password": {"root"}}.Encode())
		if e != nil || login.code != 302 {
			return rebootFailure("阿瓦隆登录失败")
		}
		token := luciToken.FindStringSubmatch(login.header.Get("Location"))
		if len(token) < 2 {
			return rebootFailure("未取得阿瓦隆会话")
		}
		response, e := c.request(ctx, "/cgi-bin/luci/;stok="+token[1]+"/admin/system/reboot?reboot=1", "GET", "")
		return rebootResponse(response, e)
	}
	if family != "AvalonDevice" {
		return rebootFailure("未识别可用的阿瓦隆重启接口")
	}
	login, e := c.request(ctx, "/login.cgi", "POST", url.Values{"username": {"root"}, "passwd": {"root"}}.Encode())
	if e != nil || login.code != 200 {
		return rebootFailure("阿瓦隆登录失败")
	}
	loginOK := strings.Contains(string(login.body), "get_minerinfo.cgi")
	if m, e := decode(login.body); e == nil {
		if success, ok := m["success"].(bool); ok && success {
			loginOK = true
		}
	}
	if !loginOK {
		return rebootFailure("阿瓦隆未确认登录成功，未发送重启命令")
	}

	info, e := c.request(ctx, "/get_minerinfo.cgi", "GET", "")
	m, parseErr := decode(info.body)
	if e != nil || info.code != 200 || parseErr != nil || text(m, "hwtype") == "" {
		return rebootFailure("未确认阿瓦隆登录会话")
	}
	response, e := c.request(ctx, "/reboot.cgi", "POST", "")
	return rebootResponse(response, e)
}
