package miner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAvalonHTTPLoginAndBoardLog(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`Avalon Device login.cgi`))
		case "/updatecgconf.cgi":
			w.Write([]byte(`CGConfCallback({"worker1":"farm.01","worker2":"farm.01","worker3":"farm.backup"});`))
		case "/get_minerinfo.cgi":
			w.Write([]byte(`{"hwtype":"Avalon 1566HA"}`))
		case "/login.cgi":
			r.ParseForm()
			if r.Form.Get("username") != "root" || r.Form.Get("passwd") != "root" {
				t.Error("credentials")
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "test", Path: "/"})
			w.Write([]byte(`{"success":true}`))
		case "/updatecglog.cgi":
			if _, e := r.Cookie("session"); e != nil {
				t.Error(e)
			}
			w.Write([]byte("log prefix 'STATS':{" + flat1566 + "} suffix"))
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	r, e := readAvalonHTTP(context.Background(), s.URL, true, nil)
	if e != nil || len(r.Boards) != 4 || !hasBoardMeans(r) {
		t.Fatalf("%v %+v", e, r)
	}
	for i, b := range r.Boards {
		value(t, b.Chip.AverageC, []float64{66, 68, 63, 64}[i])
	}
}
func TestAntminerEmbeddedLegacyTemperatures(t *testing.T) {
	r, e := ParseAntminer([]byte(`{"devs":[{"freq":"500,temp1=61,temp2=62,temp3=63"}]}`))
	if e != nil || len(r.Boards) != 3 {
		t.Fatalf("%v %+v", e, r)
	}
	for i, b := range r.Boards {
		if b.Chip.AverageC != nil {
			t.Fatal("single point became mean")
		}
		value(t, b.Other[0].ValueC, float64(61+i))
	}
}

func TestAvalonLuciExactSessionFlow(t *testing.T) {
	var paths []string
	var tracePaths []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<a href="/luci/avalon/">Login</a>`))
		case "/cgi-bin/luci/avalon/page/index":
			r.ParseForm()
			if r.Form.Get("password") != "root" || r.Form.Get("username") != "root" {
				t.Error("wrong LuCI credentials")
			}
			http.SetCookie(w, &http.Cookie{Name: "sysauth", Value: "test", Path: "/"})
			w.Header().Set("Location", "/cgi-bin/luci/;stok=privateToken/avalon/page/index")
			w.WriteHeader(302)
		case "/cgi-bin/luci/;stok=privateToken/avalon/api/getstatus":
			if _, e := r.Cookie("sysauth"); e != nil {
				t.Error(e)
			}
			w.Write([]byte(`{"openwrtver":"Avalon","ghsmm":560000,"ghsav":550000,"temp":66}`))
		default:
			t.Error("unexpected request", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	r, e := readAvalonHTTP(context.Background(), s.URL, false, func(path, method string, status int, b []byte, e error, d time.Duration) {
		tracePaths = append(tracePaths, path)
	})
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.TempC, 66)
	value(t, r.RealtimeTH, 560)
	if len(r.Boards) != 0 {
		t.Fatal("LuCI scalar invented boards")
	}
	want := []string{"/", "/cgi-bin/luci/avalon/page/index", "/cgi-bin/luci/;stok=privateToken/avalon/api/getstatus"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatal(paths)
	}
	if strings.Contains(strings.Join(tracePaths, " "), "privateToken") {
		t.Fatal("session leaked")
	}
}
func TestAvalonLogNeedsOnlyMTavg(t *testing.T) {
	var paths []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/login.cgi":
			w.Write([]byte("get_minerinfo.cgi"))
		case "/updatecgconf.cgi":
			w.Write([]byte(`CGConfCallback({"worker1":"farm.01","worker2":"farm.01","worker3":"farm.backup"});`))
		case "/get_minerinfo.cgi":
			w.Write([]byte(`{"hwtype":"1566HA"}`))
		case "/updatecglog.cgi":
			w.Write([]byte("MTavg[66 68 63 64]"))
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer s.Close()
	r, e := readAvalonHTTPFamily(context.Background(), s.URL, "AvalonDevice", true, nil)
	if e != nil || len(r.Boards) != 4 {
		t.Fatalf("%v %+v", e, r)
	}
	if !reflect.DeepEqual(paths, []string{"/login.cgi", "/get_minerinfo.cgi", "/updatecglog.cgi", "/updatecgconf.cgi"}) {
		t.Fatal(paths)
	}
}

func TestStockHTTPThreeTemperaturesSurviveSupplementalStats(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/cgi-bin/get_miner_conf.cgi":
			w.Write([]byte(`{}`))
		case "/cgi-bin/get_miner_status.cgi":
			w.Write([]byte(`{"devs":[{"freq":"500,temp1=61,temp2=62,temp3=63"}]}`))
		case "/cgi-bin/stats.cgi":
			w.Write([]byte(`{"STATS":[{}, {"temp1":71,"temp2":72,"temp3":73,"temp4":74}]}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	r, _, _ := readAntminerHTTP(context.Background(), server.URL, Credentials{"admin", "admin"})
	v := originalValues(r)
	if len(v) != 3 || v[0] != 61 || v[2] != 63 {
		t.Fatal(v)
	}
	if len(paths) < 3 || paths[0] != "/cgi-bin/get_miner_conf.cgi" || paths[1] != "/cgi-bin/get_miner_status.cgi" {
		t.Fatal(paths)
	}
}

func TestFreshAuthenticatedLogReplacesCompleteTCPZeroBoards(t *testing.T) {
	old, e := ParseCGMiner([]byte(`{"STATS":[{"MM ID0":"Ver[1566HA] GHSavg[560000] TMax[0] MTmax[0 0 0 0] MTavg[0 0 0 0]"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/login.cgi":
			r.ParseForm()
			if r.Form.Get("passwd") != "root" {
				t.Error("login")
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
			w.Write([]byte("get_minerinfo.cgi"))
		case "/updatecgconf.cgi":
			w.Write([]byte(`CGConfCallback({"worker1":"farm.01","worker2":"farm.01","worker3":"farm.backup"});`))
		case "/get_minerinfo.cgi":
			w.Write([]byte(`{"hwtype":"1566HA"}`))
		case "/updatecglog.cgi":
			if _, e := r.Cookie("session"); e != nil {
				t.Error(e)
			}
			w.Write([]byte(flat1566))
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	log, e := readAvalonHTTPFamily(context.Background(), server.URL, "AvalonDevice", true, nil)
	if e != nil {
		t.Fatal(e)
	}
	result := mergeFreshAvalonLog(old, log)
	if len(result.Boards) != 4 {
		t.Fatal(result.Boards)
	}
	for i, b := range result.Boards {
		value(t, b.Chip.AverageC, []float64{66, 68, 63, 64}[i])
	}
	value(t, result.TempC, 82)
	if len(paths) != 4 {
		t.Fatal(paths)
	}
	value(t, old.Boards[0].Chip.AverageC, 0)
}
