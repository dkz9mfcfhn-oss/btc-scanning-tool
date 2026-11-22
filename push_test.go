package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPushOutageRecoveryAndRetry(t *testing.T) {
	s := pushState{}
	s, title := pushTransition(s, false)
	if title != "" {
		t.Fatal("unknown device alerted")
	}
	s, _ = pushTransition(s, true)
	s, title = pushTransition(s, false)
	if title != "" {
		t.Fatal("single timeout alerted")
	}
	s, title = pushTransition(s, false)
	if title != "设备掉线" {
		t.Fatal(title)
	}
	// Failed delivery must remain eligible.
	s, title = pushTransition(s, false)
	if title != "设备掉线" {
		t.Fatal(title)
	}
	s.Notified = true
	s, title = pushTransition(s, false)
	if title != "" {
		t.Fatal("duplicate alert")
	}
	s, title = pushTransition(s, true)
	if title != "恢复在线" {
		t.Fatal(title)
	}
	s.Notified = false
	_, title = pushTransition(s, true)
	if title != "" {
		t.Fatal("duplicate recovery")
	}
}
func TestPushRejectsUnsafeURL(t *testing.T) {
	for _, s := range []string{"http://example.com/abcdefgh", "https://user:pass@example.com/abcdefgh", "https://example.com/key", "https://example.com/abcdefgh?token=x"} {
		if _, e := validatePushURL(s); e == nil {
			t.Fatal(s)
		}
	}
}
func TestPushPOSTAndResponse(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	count := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.Method != "POST" {
			t.Error(r.Method)
		}
		r.ParseForm()
		if r.Form.Get("title") != "测试" || strings.Contains(r.URL.RawQuery, "测试") {
			t.Error("payload")
		}
		w.Write([]byte(`{"code":200}`))
	}))
	defer srv.Close()
	http.DefaultTransport = srv.Client().Transport
	if e := sendPush(context.Background(), srv.URL+"/abcdefgh", "测试", "body"); e != nil {
		t.Fatal(e)
	}
	if count != 1 {
		t.Fatal(count)
	}
}
