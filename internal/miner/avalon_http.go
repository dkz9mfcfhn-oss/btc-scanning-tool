package miner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"minerwatch/internal/telemetry"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type btcHTTPReply struct {
	body   []byte
	code   int
	header http.Header
}
type btcHTTPClient struct {
	client *http.Client
	base   string
	trace  restTrace
}

func newBTCHTTP(base string, trace restTrace) *btcHTTPClient {
	c := restClient()
	c.Timeout = 10 * time.Second
	if tr, ok := c.Transport.(*http.Transport); ok {
		tr.ResponseHeaderTimeout = 10 * time.Second
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.Jar, _ = cookiejar.New(nil)
	return &btcHTTPClient{c, base, trace}
}
func (c *btcHTTPClient) request(ctx context.Context, path, method, body string) (btcHTTPReply, error) {
	req, e := http.NewRequestWithContext(ctx, method, c.base+path, strings.NewReader(body))
	if e != nil {
		return btcHTTPReply{}, e
	}
	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	start := time.Now()
	resp, e := c.client.Do(req)
	out := btcHTTPReply{}
	if e == nil {
		out.code = resp.StatusCode
		out.header = resp.Header
		out.body, e = io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
		resp.Body.Close()
		if len(out.body) > maxResponse {
			out.body = nil
			e = errors.New("response exceeds 2 MiB")
		}
	}
	if c.trace != nil {
		safe := out.body
		if path == "/updatecglog.cgi" && len(out.body) > 0 {
			safe, _ = json.Marshal(map[string]any{"STATS": []any{map[string]any{avalonFlatKey: string(out.body)}}})
		}
		label := path
		if method == "POST" {
			safe = nil
		}
		if strings.Contains(path, ";stok=") {
			label = "/cgi-bin/luci/;stok=REDACTED/avalon/api/getstatus"
		}
		reported := e
		if reported == nil && out.code >= 400 {
			reported = fmt.Errorf("HTTP %d", out.code)
		}
		c.trace(label, method, out.code, safe, reported, time.Since(start))
	}
	return out, e
}
func detectBTCHTTP(ctx context.Context, base string) string {
	c := newBTCHTTP(base, nil)
	defer c.client.CloseIdleConnections()
	one, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	r, e := c.request(one, "/", "GET", "")
	if e != nil {
		return ""
	}
	return btcHTTPFamily(r)
}
func btcHTTPFamily(r btcHTTPReply) string {
	body := string(r.body)
	if r.code == 401 && strings.HasPrefix(strings.ToLower(r.header.Get("WWW-Authenticate")), "digest ") {
		return "Antminer"
	}
	if r.code == 200 && strings.Contains(body, "/luci/avalon/") {
		return "AvalonLuci"
	}
	if r.code == 200 && strings.Contains(body, "Avalon Device") && strings.Contains(body, "login.cgi") {
		return "AvalonDevice"
	}
	return ""
}

// Implements both read-only Avalon scanner branches found in BTC Tools 1.3.4.
func readAvalonHTTP(ctx context.Context, base string, knownAvalon bool, trace restTrace) (Reading, error) {
	return readAvalonHTTPFamily(ctx, base, "", knownAvalon, trace)
}
func readAvalonHTTPFamily(ctx context.Context, base, family string, knownAvalon bool, trace restTrace) (Reading, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c := newBTCHTTP(base, trace)
	defer c.client.CloseIdleConnections()
	if family == "" {
		root, e := c.request(ctx, "/", "GET", "")
		if e == nil {
			family = btcHTTPFamily(root)
		}
		if family == "" && knownAvalon {
			family = "AvalonDevice"
		}
	}
	switch family {
	case "AvalonLuci":
		return c.readLuci(ctx)
	case "AvalonDevice":
		return c.readDevice(ctx, true)
	default:
		return Reading{}, errors.New("Avalon HTTP family not identified")
	}
}
func (c *btcHTTPClient) readDevice(ctx context.Context, includeWorkers bool) (Reading, error) {
	login, e := c.request(ctx, "/login.cgi", "POST", url.Values{"username": {"root"}, "passwd": {"root"}}.Encode())
	if e != nil {
		return Reading{}, e
	}
	if login.code != 200 {
		return Reading{}, fmt.Errorf("Avalon login HTTP %d", login.code)
	}
	// Follow BTC Tools' login -> minerinfo -> cglog sequence. Some firmware returns
	// JSON instead of an HTML link on successful login; actual status verifies it.
	info, e := c.request(ctx, "/get_minerinfo.cgi", "GET", "")
	if e != nil {
		return Reading{}, e
	}
	if info.code != 200 {
		return Reading{}, fmt.Errorf("Avalon minerinfo HTTP %d", info.code)
	}
	data, e := c.request(ctx, "/updatecglog.cgi", "GET", "")
	if e != nil {
		return Reading{}, e
	}
	if data.code != 200 {
		return Reading{}, fmt.Errorf("Avalon cglog HTTP %d", data.code)
	}
	var means []*float64
	for _, m := range bracket.FindAllStringSubmatch(string(data.body), -1) {
		if m[1] == "MTavg" {
			means = tempSamples(m[2])
			break
		}
	}
	valid := false
	for _, n := range means {
		if n != nil {
			valid = true
		}
	}
	encoded, _ := json.Marshal(map[string]any{"STATS": []any{map[string]any{avalonFlatKey: string(data.body)}}})
	r, e := ParseCGMiner(encoded)
	if e != nil {
		return Reading{}, e
	}
	r.Brand = "Avalon"
	r.Protocol = "Avalon Device CGI"
	if match := avalonHTTPElapsed.FindSubmatch(data.body); len(match) > 1 {
		if elapsed := elapsedSeconds(numeric(string(match[1]))); elapsed != nil {
			r.ElapsedSeconds = elapsed
		}
	}
	if valid {
		setBTCTemperatures(&r, means, "Avalon HTTP MTavg")
	}
	var model map[string]any
	_ = json.Unmarshal(info.body, &model)
	if v := text(model, "hwtype"); v != "" {
		r.Model = v
	}
	for _, item := range []struct {
		key string
		dst **float64
	}{{"MHS 5s", &r.RealtimeTH}, {"MHS av", &r.AverageTH}} {
		re := regexp.MustCompile(`'` + regexp.QuoteMeta(item.key) + `'\s*:\s*([0-9.]+)`)
		if m := re.FindSubmatch(data.body); len(m) > 1 {
			*item.dst = scale(numeric(string(m[1])), 1e-6)
		}
	}
	if !valid && !hasObservation(r) && !hasAvalonBootBy(r) {
		return Reading{}, errors.New("Avalon cglog did not return recognizable telemetry or restart history")
	}
	if includeWorkers {
		if config, err := c.request(ctx, "/updatecgconf.cgi", "GET", ""); err == nil && config.code == 200 {
			r = merge(r, avalonConfigWorkers(config.body))
		}
	}
	return r, nil
}

var avalonHTTPElapsed = regexp.MustCompile(`['"]Elapsed['"]\s*:\s*['"]?([0-9]+(?:\.[0-9]+)?)['"]?\s*[,}]`)

var luciToken = regexp.MustCompile(`/;stok=([A-Za-z0-9_-]{1,256})/`)

func (c *btcHTTPClient) readLuci(ctx context.Context) (Reading, error) {
	login, e := c.request(ctx, "/cgi-bin/luci/avalon/page/index", "POST", url.Values{"username": {"root"}, "password": {"root"}}.Encode())
	if e != nil {
		return Reading{}, e
	}
	if login.code != 302 {
		return Reading{}, fmt.Errorf("Avalon LuCI login HTTP %d", login.code)
	}
	// Use only the token, never follow the server-supplied redirect host.
	match := luciToken.FindStringSubmatch(login.header.Get("Location"))
	if len(match) < 2 {
		return Reading{}, errors.New("Avalon LuCI missing session token")
	}
	data, e := c.request(ctx, "/cgi-bin/luci/;stok="+match[1]+"/avalon/api/getstatus", "GET", "")
	if e != nil {
		return Reading{}, e
	}
	if data.code != 200 {
		return Reading{}, fmt.Errorf("Avalon LuCI status HTTP %d", data.code)
	}
	m, e := decode(data.body)
	if e != nil {
		return Reading{}, e
	}
	r := Reading{Brand: "Avalon", Model: text(m, "openwrtver"), Protocol: "Avalon LuCI", ElapsedSeconds: elapsedSeconds(number(m, "elapsed")), RealtimeTH: scale(number(m, "ghsmm"), 1e-3), AverageTH: scale(number(m, "ghsav"), 1e-3), TempC: tempValue(m["temp"])}
	for _, key := range []string{"BOOTBY", "bootby"} {
		raw := text(m, key)
		if raw == "" {
			raw = healthInteger(m[key])
		}
		addAvalonBootBy(&r, "Avalon LuCI."+key, raw)
	}
	setBTCTemperatures(&r, tempSamples(m["temp"]), "Avalon LuCI temp")
	r = merge(r, poolWorkers(m["pool"]))
	if !hasData(r) && r.TempC == nil && !hasAvalonBootBy(r) {
		return Reading{}, errors.New("Avalon LuCI response contains no telemetry")
	}
	return r, nil
}

// BOOTBY may exist only in the authenticated log. When diagnosis needs that
// last restart record, use a short read and keep the existing sensor route.
func supplementAvalonHealth(ctx context.Context, base, family string, current Reading, trace restTrace) Reading {
	if !explicitZeroHashrate(current) || hasAvalonBootBy(current) || ctx.Err() != nil {
		return current
	}
	if current.Brand != "Avalon" && !is1566(current) && !strings.HasPrefix(family, "Avalon") {
		return current
	}
	one, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	client := newBTCHTTP(base, trace)
	defer client.client.CloseIdleConnections()
	var extra Reading
	var err error
	if family == "AvalonLuci" {
		extra, err = client.readLuci(one)
	} else {
		extra, err = client.readDevice(one, false)
	}
	if err != nil {
		return current
	}
	return merge(current, Reading{Faults: extra.Faults, StatusKnown: extra.StatusKnown})
}

// A new authenticated log is a fresh snapshot, not a fill-in for TCP zeros.
func mergeFreshAvalonLog(current, log Reading) Reading {
	if btcTemperatureRank(log.Temperatures) != 2 {
		return merge(current, log)
	}
	var boards []telemetry.BoardTemperature
	for _, b := range current.Boards {
		if !strings.HasPrefix(b.ID, "avalon:") {
			boards = append(boards, b)
		}
	}
	current.Boards = boards
	var sensors []telemetry.Sensor
	for _, s := range current.Temperatures {
		if !strings.HasPrefix(s.Source, btcTemperaturePrefix) && !strings.HasPrefix(s.Source, "Avalon ") {
			sensors = append(sensors, s)
		}
	}
	current.Temperatures = sensors
	current.TempC = log.TempC
	return merge(current, log)
}
