package miner

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestFMS1566Wrappers(t *testing.T) {
	log := "Ver[1566HA-512] TMax[82] TAvg[65] MTavg[66 68 63 64] MTmax[77 82 75 79]"
	encoded, _ := json.Marshal(map[string]any{"STATS": log})
	for _, data := range []string{log, string(encoded), "STATUS=S|STATS=0,MM ID0=" + log + "|"} {
		r, e := parseAvalonFMS([]byte(data))
		if e != nil {
			t.Fatal(e)
		}
		if r.TempC == nil || *r.TempC != 82 || len(r.Boards) != 4 {
			t.Fatalf("bad reading: %+v", r)
		}
		for i, want := range []float64{66, 68, 63, 64} {
			if r.Boards[i].Chip.AverageC == nil || *r.Boards[i].Chip.AverageC != want {
				t.Fatal(r.Boards)
			}
		}
		if btcTemperatureRank(r.Temperatures) != 0 {
			t.Fatal("BTC route used")
		}
	}
	request, _ := json.Marshal(fmsRequest())
	if string(request) != `{"command":"stats","new_api":true}` {
		t.Fatal(string(request))
	}
}

func TestFMSNoInventedBoardsOrStaleZero(t *testing.T) {
	r, e := parseAvalonFMS([]byte("TMax[81]"))
	if e != nil || len(r.Boards) != 0 || *r.TempC != 81 {
		t.Fatal(r, e)
	}
	for _, data := range []string{`{"DEVS":[{"Temperature":0}]}`, "MTavg[bad]", "TMax[70] TMax[90]"} {
		fresh, err := parseAvalonFMS([]byte(data))
		if err == nil {
			t.Fatal(data)
		}
		old := Reading{Model: "1566HA", TempC: ptr(0), RealtimeTH: ptr(580)}
		got := applyAvalonFMS(old, fresh, err)
		if got.TempC != nil || *got.RealtimeTH != 580 {
			t.Fatal(got)
		}
	}
}

func TestFMSRequestOverTCP(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	requests := make(chan map[string]any, 1)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		var req map[string]any
		json.NewDecoder(c).Decode(&req)
		requests <- req
		c.Write([]byte("'STATS':{TMax[81] TAvg[65] MTavg[66 68 63 64] MTmax[77 81 75 79]}\x00"))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	got := readAvalonFMS(ctx, listener.Addr().String(), Reading{Model: "Avalon1566HA", TempC: ptr(0), RealtimeTH: ptr(582)})
	if got.TempC == nil || *got.TempC != 81 || len(got.Boards) != 4 {
		t.Fatalf("%+v", got)
	}
	select {
	case req := <-requests:
		if req["command"] != "stats" || req["new_api"] != true || len(req) != 2 {
			t.Fatal(req)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
