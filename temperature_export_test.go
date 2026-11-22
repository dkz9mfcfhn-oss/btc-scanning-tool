package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"minerwatch/internal/miner"
	"minerwatch/internal/store"
	"minerwatch/internal/telemetry"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTemperatureCSVPreservesMeansSensorsAndSource(t *testing.T) {
	a, err := newApp(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	ip := "192.168.1.10"
	a.devices[ip] = &Device{IP: ip, Model: "S19"}
	boards := []telemetry.BoardTemperature{{ID: "chain:2", Name: "板2", Chip: telemetry.TemperatureSeries{AverageC: num(62), AverageKind: "sensor_mean", Source: "stats.chain[2].temp_chip", SensorsC: []*float64{num(60), nil, num(64)}}}}
	if err = a.db.Append([]store.Record{{IP: ip, Sample: store.Sample{At: time.Now(), Online: true, Boards: boards, Temperatures: []telemetry.Sensor{{Name: "环境", ValueC: num(0), Source: "ambient"}}}}}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.export(w, httptest.NewRequest("GET", "http://local/api/export", nil))
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\xef\xbb\xbf"))).ReadAll()
	if err != nil || len(rows) != 2 || len(rows[0]) != 18 || len(rows[1]) != 18 {
		t.Fatal(rows, err)
	}
	var got []telemetry.BoardTemperature
	if err = json.Unmarshal([]byte(rows[1][12]), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || *got[0].Chip.AverageC != 62 || got[0].Chip.SensorsC[1] != nil || got[0].Chip.AverageKind != "sensor_mean" || got[0].Chip.Source != boards[0].Chip.Source {
		t.Fatal(got)
	}
	if !strings.Contains(rows[1][13], `"valueC":0`) {
		t.Fatal(rows[1][13])
	}
}
func TestSimplifiedTemperaturesKeepOtherSourcesInExpandedReport(t *testing.T) {
	boards := []telemetry.BoardTemperature{
		{ID: "antminer:legacy:1", Name: "旧接口", Chip: telemetry.TemperatureSeries{MaxC: num(70)}},
		{ID: "antminer:chain:index:0", Name: "后台", Chip: telemetry.TemperatureSeries{AverageC: num(65), AverageKind: "sensor_mean", Source: "new"}},
	}
	rows := simpleTemperatureRows(boards)
	if len(rows) != 1 || rows[0][0] != "板0" || rows[0][1] != "65.00*" {
		t.Fatal(rows)
	}
	report := temperatureReport(boards, nil)
	if !strings.Contains(report, "旧接口") || !strings.Contains(report, "后台") {
		t.Fatal("detail dropped alternate sources")
	}
	if got := compactBoardAverages(boards, false); got != "板0 65.0*" {
		t.Fatal(got)
	}
}

func TestCollectedBoardTemperaturesReachHistory(t *testing.T) {
	a, err := newApp(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	a.config.Ranges = "192.168.1.10"
	a.discover = func(context.Context, string) bool { return true }
	a.probe = func(ctx context.Context, ip string, c miner.Credentials) (miner.Reading, error) {
		return miner.Reading{Brand: "Antminer", Model: "S19", RealtimeTH: num(95), Boards: []telemetry.BoardTemperature{{ID: "chain:2", Chip: telemetry.TemperatureSeries{AverageC: num(65), AverageKind: "reported"}}}, Temperatures: []telemetry.Sensor{{Name: "环境", ValueC: num(27)}}}, nil
	}
	if err = a.startJob("scan"); err != nil {
		t.Fatal(err)
	}
	a.jobs.Wait()
	h := a.db.History("192.168.1.10", time.Now().Add(-time.Hour))
	if len(h) != 1 || len(h[0].Boards) != 1 || h[0].Boards[0].ID != "chain:2" || *h[0].Boards[0].Chip.AverageC != 65 || *h[0].Temperatures[0].ValueC != 27 {
		t.Fatal(h)
	}
}
