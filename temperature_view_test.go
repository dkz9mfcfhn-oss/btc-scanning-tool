package main

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"minerwatch/internal/miner"
	"minerwatch/internal/telemetry"
)

func TestTemperatureRowsKeepBoardIDZeroAndBothMeanKinds(t *testing.T) {
	boards := []telemetry.BoardTemperature{{
		ID: "0", Name: "算力板0",
		Chip:  telemetry.TemperatureSeries{AverageC: num(0), MinC: num(-1), MaxC: num(1), AverageKind: "reported", Source: "stats.chains[0].temp_chip_avg", SensorsC: []*float64{num(0), nil, num(1)}},
		PCB:   telemetry.TemperatureSeries{AverageC: num(31), MinC: num(30), MaxC: num(32), AverageKind: "sensor_mean", Source: "stats.chains[0].temp_pcb", SensorsC: []*float64{num(30), num(32)}},
		Other: []telemetry.Sensor{{Name: "进水", ValueC: num(20), Source: "edevs[0].water_inlet"}},
	}}
	rows := temperatureRows(boards)
	if len(rows) != 1 || len(rows[0]) != 10 {
		t.Fatalf("unexpected temperature table shape: %q", rows)
	}
	row := rows[0]
	want := []string{"算力板0 [ID: 0]", "0.00", "-1.00", "1.00", "31.00", "30.00", "32.00", "芯片: 固件；PCB: 采样", "芯片 2/3；PCB 2/2"}
	if !reflect.DeepEqual(row[:9], want) {
		t.Fatalf("temperature table lost data\ngot %q\nwant %q", row[:9], want)
	}
	for _, source := range []string{boards[0].Chip.Source, boards[0].PCB.Source, boards[0].Other[0].Source} {
		if !strings.Contains(row[9], source) {
			t.Errorf("field provenance missing: %q", source)
		}
	}
}

func TestTemperatureRowsDoNotInferMissingIDsOrMeans(t *testing.T) {
	boards := []telemetry.BoardTemperature{
		{Name: "单点来源", Chip: telemetry.TemperatureSeries{SensorsC: []*float64{num(70)}, MinC: num(70), MaxC: num(70), Source: "summary.chip_temp"}},
		{ID: "5", Name: "仅极值", Chip: telemetry.TemperatureSeries{MinC: num(60), MaxC: num(80)}, PCB: telemetry.TemperatureSeries{MinC: num(30), MaxC: num(40)}},
		{ID: "9", Name: "未完成的多点模型", Chip: telemetry.TemperatureSeries{SensorsC: []*float64{num(60), num(80)}}},
	}
	before := telemetry.CloneBoards(boards)
	rows := temperatureRows(boards)
	if !strings.Contains(rows[0][0], "ID: 未上报") {
		t.Fatalf("missing ID replaced with an invented board index: %q", rows[0][0])
	}
	for _, row := range rows {
		if row[1] != "未上报" || row[4] != "未上报" || row[7] != "芯片: 未上报；PCB: 未上报" {
			t.Fatalf("view inferred a board mean: %q", row)
		}
	}
	_ = temperatureReport(boards, nil)
	if !reflect.DeepEqual(boards, before) {
		t.Fatal("display mutated telemetry model")
	}
}

func TestTemperatureReportPreservesEverySensorIndexAndSource(t *testing.T) {
	boards := []telemetry.BoardTemperature{
		{ID: "2", Name: "板 2", Chip: telemetry.TemperatureSeries{AverageC: num(65), MinC: num(0), MaxC: num(72), AverageKind: "reported", Source: "stats.MM ID0.MTavg[2] / PVT_T2", SensorsC: []*float64{num(0), nil, num(72), num(61.25)}}, PCB: telemetry.TemperatureSeries{AverageC: num(32), AverageKind: "sensor_mean", Source: "HTTP.chains[2].pcb", SensorsC: []*float64{num(31), num(33)}}, Other: []telemetry.Sensor{{Name: "环境", ValueC: num(0), Source: "stats.Temp"}, {Name: "出口", ValueC: nil, Source: "edevs[2].outlet"}}},
		{ID: "2", Name: "另一接口板2", Chip: telemetry.TemperatureSeries{AverageC: num(64), AverageKind: "reported", Source: "edevs[2].chip_temp_avg"}},
		{Name: "缺失板号来源", Chip: telemetry.TemperatureSeries{Source: "summary.temperature"}},
	}
	sensors := []telemetry.Sensor{{Name: "整机进水", ValueC: num(25), Source: "summary.water_temp_in"}, {Name: "整机出水", ValueC: nil, Source: "summary.water_temp_out"}, {Name: "环境零点", ValueC: num(0), Source: "stats.ambient"}}
	report := temperatureReport(boards, sensors)
	for _, want := range []string{
		"板号 ID: 2", "名称 Name: 板 2", "名称 Name: 另一接口板2", "板号 ID: 未上报", "名称 Name: 缺失板号来源",
		"芯片范围: 0.00 °C ～ 72.00 °C", "芯片 SensorsC[0]: 0.00 °C", "芯片 SensorsC[1]: 未上报", "芯片 SensorsC[2]: 72.00 °C", "芯片 SensorsC[3]: 61.25 °C",
		"PCB SensorsC[0]: 31.00 °C", "PCB SensorsC[1]: 33.00 °C", "芯片有效采样点数: 3/4", "PCB有效采样点数: 2/2", "AverageKind: reported", "AverageKind: sensor_mean",
		"Other[0]: 名称 Name: 环境；温度 ValueC: 0.00 °C", "Other[1]: 名称 Name: 出口；温度 ValueC: 未上报",
		"Sensors[0]: 名称 Name: 整机进水；温度 ValueC: 25.00 °C", "Sensors[1]: 名称 Name: 整机出水；温度 ValueC: 未上报", "Sensors[2]: 名称 Name: 环境零点；温度 ValueC: 0.00 °C",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q\n%s", want, report)
		}
	}
	for _, board := range boards {
		for _, source := range []string{board.Chip.Source, board.PCB.Source} {
			if source != "" && !strings.Contains(report, source) {
				t.Errorf("missing source %q", source)
			}
		}
		for _, s := range board.Other {
			if !strings.Contains(report, s.Source) {
				t.Errorf("missing Other source %q", s.Source)
			}
		}
	}
	for _, s := range sensors {
		if !strings.Contains(report, s.Source) {
			t.Errorf("missing machine source %q", s.Source)
		}
	}
	if strings.Contains(strings.ReplaceAll(report, "\r\n", ""), "\n") || !strings.HasSuffix(report, "\r\n") {
		t.Fatal("report is not CRLF Windows text")
	}
}

func TestTemperatureViewUnknownMeanKindIsNotRelabeled(t *testing.T) {
	boards := []telemetry.BoardTemperature{{Name: "板", Chip: telemetry.TemperatureSeries{AverageC: num(66), AverageKind: "vendor_interval"}, PCB: telemetry.TemperatureSeries{AverageC: num(35)}}}
	row := temperatureRows(boards)[0]
	if row[7] != "芯片: 口径未知: vendor_interval；PCB: 口径未上报" {
		t.Fatalf("unknown mean provenance mislabeled: %q", row[7])
	}
	if !strings.Contains(boardAverageSummary(boards, false), "口径未知: vendor_interval") {
		t.Fatal("list summary hid unknown mean kind")
	}
}

func TestTemperatureViewInvalidValuesRetainSlotsWithoutCountingThem(t *testing.T) {
	s := telemetry.TemperatureSeries{SensorsC: []*float64{nil, num(0), num(math.NaN()), num(201), num(-51), num(math.Inf(1))}}
	board := telemetry.BoardTemperature{Name: "诊断板", Chip: s}
	row := temperatureRows([]telemetry.BoardTemperature{board})[0]
	if row[8] != "芯片 1/6；PCB 0/0" {
		t.Fatalf("invalid sensors counted: %q", row[8])
	}
	report := temperatureReport([]telemetry.BoardTemperature{board}, nil)
	for i, want := range []string{"未上报", "0.00 °C", "未上报", "未上报", "未上报", "未上报"} {
		line := "芯片 SensorsC[" + string(rune('0'+i)) + "]: " + want
		if !strings.Contains(report, line) {
			t.Errorf("missing preserved slot %q", line)
		}
	}
}

func TestTemperatureReportEmptyAndEmbeddedNewlines(t *testing.T) {
	if rows := temperatureRows(nil); len(rows) != 0 {
		t.Fatalf("no boards created table rows: %q", rows)
	}
	empty := temperatureReport(nil, nil)
	if !strings.Contains(empty, "【各板温度】\r\n未上报") || !strings.Contains(empty, "【整机温度传感器】\r\n未上报") {
		t.Fatalf("empty report missing unavailable states: %q", empty)
	}
	report := temperatureReport([]telemetry.BoardTemperature{{ID: "board\n2", Name: "name\r\nsource", Chip: telemetry.TemperatureSeries{Source: "one\rtwo\nthree"}}}, nil)
	if strings.Contains(strings.ReplaceAll(report, "\r\n", ""), "\n") || strings.Contains(strings.ReplaceAll(report, "\r\n", ""), "\r") {
		t.Fatal("source fields broke CRLF line layout")
	}
	for _, want := range []string{`board\n2`, `name\nsource`, `one\rtwo\nthree`} {
		if !strings.Contains(report, want) {
			t.Errorf("newline source information was lost: %q", want)
		}
	}
}

func TestFlatAvalonFourBoardList(t *testing.T) {
	r, err := miner.ParseCGMiner([]byte(`'STATS':{Ver[1566HA-512] GHSavg[567594.80] MTavg[66 68 63 64] MTmax[77 82 75 79]`))
	if err != nil {
		t.Fatal(err)
	}
	want := "板0 66.0  |  板1 68.0  |  板2 63.0  |  板3 64.0"
	if got := compactBoardAverages(r.Boards, false); got != want {
		t.Fatal(got)
	}
	// Different protocol representations must not produce duplicate board rows.
	mixed := append([]telemetry.BoardTemperature{{ID: "avalon:mm:0:board:0", Chip: telemetry.TemperatureSeries{MaxC: num(77)}}}, r.Boards...)
	if got := compactBoardAverages(mixed, false); got != want {
		t.Fatal(got)
	}
}

func TestSingleBoardTemperatureVisibleWithoutFalseMean(t *testing.T) {
	r, e := miner.ParseAntminer([]byte(`{"devs":[{"freq":"500,temp1=61,temp2=62,temp3=63"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	got := compactBoardAverages(r.Boards, false)
	for _, want := range []string{"板1 61.0（测温）", "板2 62.0（测温）", "板3 63.0（测温）"} {
		if !strings.Contains(got, want) {
			t.Fatal(got)
		}
	}
}

func TestThreeExplicitChainsOverrideFourLegacySensorRecords(t *testing.T) {
	var boards []telemetry.BoardTemperature
	for _, id := range []string{"0", "1", "2"} {
		boards = append(boards, telemetry.BoardTemperature{ID: "antminer:chain:index:" + id, Chip: telemetry.TemperatureSeries{MaxC: num(65)}})
	}
	for _, id := range []string{"0", "1", "2", "3"} {
		boards = append(boards, telemetry.BoardTemperature{ID: "antminer:legacy:" + id, Chip: telemetry.TemperatureSeries{AverageC: num(66)}})
	}
	chosen := preferredTemperatureBoards(boards)
	if len(chosen) != 3 {
		t.Fatal(chosen)
	}
	for _, b := range chosen {
		if !strings.HasPrefix(b.ID, "antminer:chain:") {
			t.Fatal(b)
		}
	}
}

func TestBTCOriginalTemperatureDisplayDoesNotInventFourthBoard(t *testing.T) {
	r, e := miner.ParseAntminer([]byte(`{"STATS":[{}, {"temp0":90,"temp1":61,"temp2":62,"temp3":63,"temp_chip4":"80-81"}],"devs":[{"freq":"500,temp1=64,temp2=65,temp3=66"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	if got := btcTemperatureText(r.Temperatures); got != "64.0 / 65.0 / 66.0" {
		t.Fatal(got)
	}
	if len(displayTemperatureBoards(r.Boards, r.Temperatures)) != 0 {
		t.Fatal("legacy sensor fields shown as physical boards")
	}
}

func Test1566ActualLogShowsFourOriginalMeans(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"STATS": []any{map[string]any{"MM ID0": "Ver[1566HA] GHSavg[567594.80] TMax[82] MTmax[77 82 75 79] MTavg[66 68 63 64]"}}})
	r, e := miner.ParseCGMiner(payload)
	if e != nil {
		t.Fatal(e)
	}
	if got := btcTemperatureText(r.Temperatures); got != "66.0 / 68.0 / 63.0 / 64.0" {
		t.Fatal(got)
	}
	if len(simpleTemperatureRows(displayTemperatureBoards(r.Boards, r.Temperatures))) != 4 {
		t.Fatal(r.Boards)
	}
}
