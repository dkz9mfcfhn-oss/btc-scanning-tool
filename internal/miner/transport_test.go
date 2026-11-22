package miner

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mockTCP(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		c, e := l.Accept()
		if e == nil {
			defer c.Close()
			serve(c)
		}
	}()
	return l.Addr().String()
}
func TestFramedFragmentationAndHeartbeat(t *testing.T) {
	addr := mockTCP(t, func(c net.Conn) {
		var head [4]byte
		io.ReadFull(c, head[:])
		n := binary.LittleEndian.Uint32(head[:])
		b := make([]byte, n)
		io.ReadFull(c, b)
		if string(b) != `{"cmd":"get.miner.status"}` {
			return
		}
		reply := []byte(`{"code":0,"msg":{"summary":{"hash-average":1}}}`)
		c.Write([]byte{0, 0, 0, 0})
		binary.LittleEndian.PutUint32(head[:], uint32(len(reply)))
		for _, b := range append(head[:], reply...) {
			c.Write([]byte{b})
		}
	})
	b, e := tcpJSON(context.Background(), addr, map[string]string{"cmd": "get.miner.status"}, true)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(b), "hash-average") {
		t.Fatal(string(b))
	}
}
func TestRawStopsWithoutWaitingForClose(t *testing.T) {
	addr := mockTCP(t, func(c net.Conn) {
		b := make([]byte, 1024)
		c.Read(b)
		c.Write([]byte(`{"SUMMARY":[{"MHS 5s":1000000}]}`))
		time.Sleep(500 * time.Millisecond)
	})
	start := time.Now()
	_, e := tcpJSON(context.Background(), addr, map[string]string{"command": "summary"}, false)
	if e != nil {
		t.Fatal(e)
	}
	if time.Since(start) > 400*time.Millisecond {
		t.Fatal("waited for socket close")
	}
}
func TestFramedOversizeRejected(t *testing.T) {
	addr := mockTCP(t, func(c net.Conn) {
		b := make([]byte, 1024)
		c.Read(b)
		var h [4]byte
		binary.LittleEndian.PutUint32(h[:], maxResponse+1)
		c.Write(h[:])
	})
	_, e := tcpJSON(context.Background(), addr, map[string]string{"cmd": "get.device.info"}, true)
	if e == nil || !strings.Contains(e.Error(), "2 MiB") {
		t.Fatal(e)
	}
}
func TestCancelInterruptsSocket(t *testing.T) {
	addr := mockTCP(t, func(c net.Conn) { b := make([]byte, 1024); c.Read(b); time.Sleep(time.Second) })
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(40 * time.Millisecond); cancel() }()
	start := time.Now()
	_, e := tcpJSON(ctx, addr, map[string]string{"command": "summary"}, false)
	if e == nil {
		t.Fatal("expected canceled socket")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("cancellation did not interrupt read")
	}
}
func TestDigestHTTPChallengeResponse(t *testing.T) {
	requests := 0
	hash := func(s string) string { v := md5.Sum([]byte(s)); return hex.EncodeToString(v[:]) }
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="miner", nonce="nonce1", qop="auth,auth-int", algorithm=MD5, opaque="opaque1"`)
			w.WriteHeader(401)
			return
		}
		p := map[string]string{}
		for _, m := range digestFields.FindAllStringSubmatch(auth, -1) {
			v := m[2]
			if v == "" {
				v = m[3]
			}
			p[m[1]] = v
		}
		expected := hash(hash("root:miner:secret") + ":nonce1:" + p["nc"] + ":" + p["cnonce"] + ":auth:" + hash("GET:/cgi-bin/stats.cgi"))
		if p["response"] != expected || p["uri"] != r.URL.Path || p["opaque"] != "opaque1" {
			w.WriteHeader(403)
			return
		}
		fmt.Fprint(w, `{"STATS":[{"rate_unit":"TH/s","rate_5s":95}]}`)
	}))
	defer s.Close()
	b, e := httpJSON(context.Background(), s.URL, "/cgi-bin/stats.cgi", Credentials{"root", "secret"})
	if e != nil {
		t.Fatal(e)
	}
	r, e := ParseAntminer(b)
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.RealtimeTH, 95)
	if requests != 2 {
		t.Fatalf("requests=%d", requests)
	}
}
func TestHTTPDoesNotForwardCredentialsOnRedirect(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect target must not receive a request") }))
	defer destination.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer s.Close()
	_, e := httpJSON(context.Background(), s.URL, "/cgi-bin/stats.cgi", Credentials{"root", "secret"})
	if e == nil {
		t.Fatal("expected redirect refusal")
	}
}
func TestNoCredentialsNotGuessed(t *testing.T) {
	requests := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("WWW-Authenticate", `Digest realm="miner",nonce="nonce"`)
		w.WriteHeader(401)
	}))
	defer s.Close()
	_, e := httpJSON(context.Background(), s.URL, "/cgi-bin/stats.cgi", Credentials{})
	if e == nil || requests != 1 {
		t.Fatal(e, requests)
	}
}
func TestProbeRejectsHostname(t *testing.T) {
	if _, e := Probe(context.Background(), "not-a-miner.example", Credentials{}); e == nil {
		t.Fatal("expected literal IPv4 requirement")
	}
}
