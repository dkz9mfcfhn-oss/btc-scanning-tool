package miner

import (
	"context"
	"encoding/json"
	"minerwatch/internal/telemetry"
	"net"
	"testing"
)

func TestAvalonDetailedTemperatureCompletesPartialBoards(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		var q map[string]string
		e = json.NewDecoder(c).Decode(&q)
		if e == nil && q["command"] != "estats" {
			t.Error(q)
		}
		if e == nil {
			_, e = c.Write([]byte(`{"STATS":[{"MM ID0":"MTavg[65 66 67] MTmax[75 76 77] PVT_T0[60 65 70] PVT_T1[61 66 71] PVT_T2[62 67 72]"}]}`))
		}
		done <- e
	}()
	r, e := ParseCGMiner([]byte(`{"STATS":[{"Type":"Avalon 1566HA","MM ID0":"TAvg[66] MTavg[65] MTmax[75 76 77]"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	if hasBoardMeans(r) {
		t.Fatal("one board's average passed as all board means")
	}
	r = readAvalonDetail(context.Background(), listener.Addr().String(), r)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if !hasBoardMeans(r) || len(r.Boards) != 3 {
		t.Fatal(r)
	}
	for i, b := range r.Boards {
		value(t, b.Chip.AverageC, 65+float64(i))
		if b.Chip.AverageKind != "reported" {
			t.Fatal(b)
		}
	}
}
func TestBoardMeanCompletenessDoesNotUseMachineTemperature(t *testing.T) {
	if hasBoardMeans(Reading{TempC: ptr(66)}) {
		t.Fatal("used machine temperature")
	}
	if hasBoardMeans(Reading{Boards: []telemetry.BoardTemperature{{Chip: telemetry.TemperatureSeries{AverageC: ptr(65)}}, {Chip: telemetry.TemperatureSeries{MaxC: ptr(75)}}}}) {
		t.Fatal("incomplete means")
	}
}

func TestFlat1566DetailedTemperatureOverTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		var q map[string]string
		if e = json.NewDecoder(c).Decode(&q); e == nil {
			_, e = c.Write([]byte("'STATS':{" + flat1566 + "}\x00"))
		}
		done <- e
	}()
	r := readAvalonDetail(context.Background(), listener.Addr().String(), Reading{Brand: "Avalon"})
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if len(r.Boards) != 4 || !hasBoardMeans(r) {
		t.Fatal(r)
	}
	for i, b := range r.Boards {
		value(t, b.Chip.AverageC, []float64{66, 68, 63, 64}[i])
	}
}
