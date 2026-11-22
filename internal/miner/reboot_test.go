package miner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAntminerRebootDigestOnceAndNoRetryOnDisconnect(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		var commands atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/cgi-bin/reboot.cgi" || r.Method != "GET" {
				t.Error(r.URL.Path, r.Method)
			}
			if r.Header.Get("Authorization") == "" {
				w.Header().Set("WWW-Authenticate", `Digest realm="miner", nonce="n", qop="auth"`)
				w.WriteHeader(401)
				return
			}
			if !strings.Contains(r.Header.Get("Authorization"), `username="admin"`) {
				t.Error("missing username")
			}
			commands.Add(1)
			if disconnect {
				conn, _, _ := w.(http.Hijacker).Hijack()
				conn.Close()
				return
			}
			w.Write([]byte("rebooting"))
		}))
		c := newBTCHTTP(server.URL, nil)
		result := rebootAntminer(context.Background(), c, Credentials{"admin", "admin"})
		c.client.CloseIdleConnections()
		server.Close()
		want := "accepted"
		if disconnect {
			want = "unknown"
		}
		if result.State != want || commands.Load() != 1 {
			t.Fatal(result, commands.Load())
		}
	}
}
func TestAvalonRebootLoginRequiredAndSinglePOST(t *testing.T) {
	for _, reject := range []bool{false, true} {
		var commands atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/login.cgi":
				r.ParseForm()
				if r.Form.Get("passwd") != "root" {
					t.Error("wrong credentials")
				}
				if reject {
					w.WriteHeader(403)
					return
				}
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
				w.Write([]byte("get_minerinfo.cgi"))
			case "/get_minerinfo.cgi":
				w.Write([]byte(`{"hwtype":"1566HA"}`))
			case "/reboot.cgi":
				if r.Method != "POST" {
					t.Error(r.Method)
				}
				if _, e := r.Cookie("session"); e != nil {
					t.Error(e)
				}
				commands.Add(1)
				w.Write([]byte("ok"))
			default:
				t.Error("unexpected endpoint", r.URL.Path)
			}
		}))
		c := newBTCHTTP(server.URL, nil)
		r := rebootAvalon(context.Background(), c, "1566HA")
		c.client.CloseIdleConnections()
		server.Close()
		if reject {
			if commands.Load() != 0 || r.State != "failed" {
				t.Fatal(r)
			}
		} else if commands.Load() != 1 || r.State != "accepted" {
			t.Fatal(r, commands.Load())
		}
	}
}
func TestWhatsminerRebootCookieAndToken(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/luci":
			r.ParseForm()
			if r.Form.Get("luci_username") != "admin" {
				t.Error("credentials")
			}
			http.SetCookie(w, &http.Cookie{Name: "sysauth", Value: "ok", Path: "/"})
			w.WriteHeader(302)
		case "/cgi-bin/luci/admin/system/reboot":
			w.Write([]byte(`token: 'private-token'`))
		case "/cgi-bin/luci/admin/system/reboot/call":
			r.ParseForm()
			if r.Form.Get("token") != "private-token" {
				t.Error("token")
			}
			if _, e := r.Cookie("sysauth"); e != nil {
				t.Error(e)
			}
			calls.Add(1)
			w.Write([]byte("ok"))
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer server.Close()
	c := newBTCHTTP(server.URL, nil)
	defer c.client.CloseIdleConnections()
	result := rebootWhatsminerWeb(context.Background(), c, Credentials{"admin", "admin"})
	if result.State != "accepted" || calls.Load() != 1 || strings.Contains(result.Message, "private-token") {
		t.Fatal(result)
	}
}
func TestRESTRebootIdentityUnlockBearer(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/info":
			w.Write([]byte(`{"fw_name":"Byteon"}`))
		case "/api/v1/unlock":
			w.Write([]byte(`{"token":"private-token"}`))
		case "/api/v1/system/reboot":
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer private-token" {
				t.Error("method/auth")
			}
			calls.Add(1)
			w.WriteHeader(204)
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer server.Close()
	c := newBTCHTTP(server.URL, nil)
	defer c.client.CloseIdleConnections()
	result := rebootREST(context.Background(), c, Credentials{"admin", "admin"})
	if result.State != "accepted" || calls.Load() != 1 {
		t.Fatal(result)
	}
}

func TestRebootApplicationFailureIsNotAccepted(t *testing.T) {
	for _, body := range []string{`{"success":false}`, `{"error":"denied"}`, `{"code":-1}`} {
		if result := rebootResponse(btcHTTPReply{code: 200, body: []byte(body)}, nil); result.State != "failed" {
			t.Fatal(result)
		}
	}
}
func TestAvalonLoginPageDoesNotTriggerReboot(t *testing.T) {
	var commands atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login.cgi" {
			w.Write([]byte("Wrong password, please login"))
			return
		}
		commands.Add(1)
		w.Write([]byte(`{"hwtype":"1566HA"}`))
	}))
	defer server.Close()
	c := newBTCHTTP(server.URL, nil)
	defer c.client.CloseIdleConnections()
	result := rebootAvalon(context.Background(), c, "1566HA")
	if result.State != "failed" || commands.Load() != 0 {
		t.Fatal(result, commands.Load())
	}
}
