package miner

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const maxResponse = 2 << 20
const requestTimeout = 3 * time.Second

func dial(ctx context.Context, address string) (net.Conn, error) {
	d := net.Dialer{Timeout: 1500 * time.Millisecond}
	c, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(requestTimeout)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if err = c.SetDeadline(deadline); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func tcpJSON(ctx context.Context, address string, request any, framed bool) ([]byte, error) {
	c, err := dial(ctx, address)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if framed {
		var header [4]byte
		binary.LittleEndian.PutUint32(header[:], uint32(len(data)))
		data = append(header[:], data...)
	}
	if _, err = io.Copy(c, bytes.NewReader(data)); err != nil {
		return nil, err
	}
	if framed {
		for heartbeat := 0; heartbeat < 8; heartbeat++ {
			var header [4]byte
			if _, err = io.ReadFull(c, header[:]); err != nil {
				return nil, err
			}
			n := binary.LittleEndian.Uint32(header[:])
			if n == 0 {
				continue
			}
			if n > maxResponse {
				return nil, errors.New("response exceeds 2 MiB")
			}
			out := make([]byte, int(n))
			_, err = io.ReadFull(c, out)
			return bytes.TrimRight(out, "\x00\r\n "), err
		}
		return nil, errors.New("too many empty heartbeat frames")
	}
	var out []byte
	buf := make([]byte, 8192)
	for len(out) <= maxResponse {
		n, e := c.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
			if len(out) > maxResponse {
				return nil, errors.New("response exceeds 2 MiB")
			}
			if i := bytes.IndexByte(out, 0); i >= 0 {
				return bytes.TrimSpace(out[:i]), nil
			}
			if json.Valid(out) {
				return bytes.TrimSpace(out), nil
			}
		}
		if e != nil {
			if e == io.EOF && len(out) > 0 {
				return bytes.TrimSpace(out), nil
			}
			return nil, e
		}
	}
	return nil, errors.New("response exceeds 2 MiB")
}

func httpJSON(ctx context.Context, base, path string, creds Credentials) ([]byte, error) {
	return httpJSONWithChallenge(ctx, base, path, creds, nil)
}
func httpJSONWithChallenge(ctx context.Context, base, path string, creds Credentials, shared *string) ([]byte, error) {
	body, err := httpGetWithChallenge(ctx, base, path, creds, shared, "application/json")
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, errors.New("矿机接口未返回 JSON")
	}
	return body, nil
}

func httpGetWithChallenge(ctx context.Context, base, path string, creds Credentials, shared *string, accept string) ([]byte, error) {
	tr := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 1500 * time.Millisecond}).DialContext, DisableKeepAlives: true, ResponseHeaderTimeout: 10 * time.Second}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("miner redirect refused") }}
	var auth string
	if shared != nil && *shared != "" {
		var e error
		auth, e = digestHeader(*shared, creds, "GET", path)
		if e != nil {
			return nil, e
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return nil, err
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		req.Header.Set("Accept", accept)
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			challenge := resp.Header.Get("WWW-Authenticate")
			if shared != nil {
				*shared = challenge
			}
			resp.Body.Close()
			if creds.Username == "" {
				return nil, errors.New("矿机后台需要用户名和密码")
			}
			auth, err = digestHeader(challenge, creds, "GET", path)
			if err != nil {
				return nil, err
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if len(body) > maxResponse {
			return nil, errors.New("response exceeds 2 MiB")
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("矿机 HTTP %d", resp.StatusCode)
		}
		return body, nil
	}
	return nil, errors.New("矿机后台认证失败")
}

var digestFields = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9_-]*)\s*=\s*(?:"((?:\\.|[^"\\])*)"|([^,\s]+))`)

func digestHeader(challenge string, creds Credentials, method, uri string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "digest ") {
		return "", errors.New("矿机认证方式不是 HTTP Digest")
	}
	p := map[string]string{}
	for _, match := range digestFields.FindAllStringSubmatch(challenge[7:], -1) {
		v := match[2]
		if v == "" {
			v = match[3]
		}
		p[strings.ToLower(match[1])] = v
	}
	if p["nonce"] == "" {
		return "", errors.New("Digest nonce missing")
	}
	alg := strings.ToUpper(p["algorithm"])
	if alg == "" {
		alg = "MD5"
	}
	var hash func(string) string
	switch alg {
	case "MD5", "MD5-SESS":
		hash = func(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
	case "SHA-256", "SHA-256-SESS":
		hash = func(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
	default:
		return "", errors.New("unsupported Digest algorithm")
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	cnonce := hex.EncodeToString(b)
	qop := ""
	if p["qop"] != "" {
		for _, x := range strings.Split(p["qop"], ",") {
			if strings.TrimSpace(x) == "auth" {
				qop = "auth"
			}
		}
		if qop == "" {
			return "", errors.New("unsupported Digest qop")
		}
	}
	ha1 := hash(creds.Username + ":" + p["realm"] + ":" + creds.Password)
	if strings.HasSuffix(alg, "-SESS") {
		ha1 = hash(ha1 + ":" + p["nonce"] + ":" + cnonce)
	}
	ha2 := hash(method + ":" + uri)
	response := hash(ha1 + ":" + p["nonce"] + ":" + ha2)
	if qop != "" {
		response = hash(ha1 + ":" + p["nonce"] + ":00000001:" + cnonce + ":auth:" + ha2)
	}
	quote := func(s string) string {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", "", "\n", "").Replace(s) + `"`
	}
	auth := "Digest username=" + quote(creds.Username) + ", realm=" + quote(p["realm"]) + ", nonce=" + quote(p["nonce"]) + ", uri=" + quote(uri) + ", response=" + quote(response) + ", algorithm=" + alg
	if qop != "" {
		auth += ", qop=auth, nc=00000001, cnonce=" + quote(cnonce)
	} else if strings.HasSuffix(alg, "-SESS") {
		auth += ", cnonce=" + quote(cnonce)
	}
	if p["opaque"] != "" {
		auth += ", opaque=" + quote(p["opaque"])
	}
	return auth, nil
}
