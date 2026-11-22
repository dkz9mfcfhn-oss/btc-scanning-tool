package miner

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAntminerEveryBoardSamplesAndMissingSlots(t *testing.T) {
	r, err := ParseAntminer([]byte(`{"STATS":[{"chain":[{"index":0,"temp_chip":[60,64,null,255],"temp_pcb":[40,44],"temp_pic":[32,33]},{"index":2,"temp_chip":[70],"temp_pcb":[]}],"ambient_temp":27}]}`))
	if err != nil || len(r.Boards) != 2 {
		t.Fatal(r, err)
	}
	a, b := r.Boards[0], r.Boards[1]
	value(t, a.Chip.AverageC, 62)
	value(t, a.PCB.AverageC, 42)
	if a.Chip.AverageKind != "sensor_mean" || len(a.Chip.SensorsC) != 4 || a.Chip.SensorsC[2] != nil || a.Chip.SensorsC[3] != nil {
		t.Fatal(a)
	}
	if !strings.Contains(b.ID, "index:2") || b.Chip.AverageC != nil {
		t.Fatal("single point or missing board renumbered", b)
	}
	value(t, b.Chip.MaxC, 70)
	if len(a.Other) != 2 || len(r.Temperatures) != 1 {
		t.Fatal("PIC or ambient lost", r)
	}
}
func TestAntminerLegacyKeepsBoardNumbersAndSeparateFields(t *testing.T) {
	r, err := ParseAntminer([]byte(`{"STATS":[{"temp2_1":"60,64","temp_pcb1":"40,42","temp3_1":"80,82","temp2_3":"0,2"}]}`))
	if err != nil || len(r.Boards) != 2 {
		t.Fatal(r, err)
	}
	value(t, r.Boards[0].Chip.AverageC, 62)
	value(t, r.Boards[0].PCB.AverageC, 41)
	value(t, r.Boards[1].Chip.AverageC, 1)
	if !strings.HasSuffix(r.Boards[1].ID, ":3") || len(r.Boards[0].Other) != 2 {
		t.Fatal(r.Boards)
	}
}
func TestWhatsminerV3BoardMeansAreNotMachineAverage(t *testing.T) {
	r, err := ParseWhatsminerV3([]byte(`{"code":0,"msg":{"summary":{"hash-realtime":390,"chip-temp-avg":92.9,"board-temperature":[40,42],"environment-temperature":25},"edevs":[{"id":0,"slot":0,"chip-temp-avg":92.2,"chip-temp-min":80,"chip-temp-max":105},{"id":1,"slot":2,"chip-temp-avg":95.7,"chip-temp-min":83,"chip-temp-max":110}]}}`), []byte(`{"code":0,"msg":{"power":{"temp0":44}}}`))
	if err != nil || len(r.Boards) != 2 {
		t.Fatal(r, err)
	}
	value(t, r.Boards[0].Chip.AverageC, 92.2)
	value(t, r.Boards[1].Chip.AverageC, 95.7)
	if !strings.Contains(r.Boards[1].ID, "slot:2") || r.Boards[0].PCB.AverageC != nil || r.InletC != nil {
		t.Fatal(r)
	}
	if r.Boards[0].Chip.AverageKind != "reported" {
		t.Fatal(r.Boards[0])
	}
	raw, _ := json.Marshal(r.Temperatures)
	for _, want := range []string{"92.9", "板温序列", "电源温度", "44"} {
		if !strings.Contains(string(raw), want) {
			t.Fatal(want, string(raw))
		}
	}
}
func TestWhatsminerOldPerBoardMeansAndOutlet(t *testing.T) {
	r, err := ParseCGMiner([]byte(`{"DEVS":[{"ASC":0,"Slot":1,"Chip Temp Avg":70,"Chip Temp Min":61,"Chip Temp Max":77,"Temperature":40},{"ASC":1,"Slot":3,"Chip Temp Avg":0,"Temperature":42}]}`))
	if err != nil || len(r.Boards) != 2 {
		t.Fatal(r, err)
	}
	value(t, r.Boards[0].Chip.AverageC, 70)
	value(t, r.Boards[1].Chip.AverageC, 0)
	if r.Boards[0].PCB.AverageC != nil || r.OutletC != nil || len(r.Boards[0].Other) != 1 {
		t.Fatal("outlet confused with chip mean/water", r)
	}
}
func TestAvalonModuleBoardMeanMaxAndAllChips(t *testing.T) {
	r, err := ParseCGMiner([]byte(`{"STATS":[{"MM ID0":"TAvg[64] TMax[74] Temp[27] MTavg[65 bad 66] MTmax[74 72 74] PVT_T0[60 66 69] PVT_T2[62 66 70]","MM ID2":"MTavg[67] MTmax[75]"}]}`))
	if err != nil || len(r.Boards) != 4 {
		t.Fatal(r, err)
	}
	value(t, r.Boards[0].Chip.AverageC, 65)
	if r.Boards[0].Chip.AverageKind != "reported" || r.Boards[1].Chip.AverageC != nil || !strings.Contains(r.Boards[3].ID, "mm:2:") {
		t.Fatal(r.Boards)
	}
	value(t, r.Boards[2].Chip.AverageC, 66)
	value(t, r.Boards[0].Chip.MinC, 60)
	if len(r.Boards[0].Chip.SensorsC) != 3 {
		t.Fatal("chip readings lost")
	}
}
func TestAvalonNoBoardMeanFromMachineAverageOrExtrema(t *testing.T) {
	for _, raw := range []string{`{"STATS":[{"MM ID0":"TAvg[65] TMax[75]"}]}`, `{"STATS":[{"MM ID0":"MTmax[75 76]"}]}`} {
		r, err := ParseCGMiner([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range r.Boards {
			if b.Chip.AverageC != nil {
				t.Fatal("invented average", b)
			}
		}
	}
	r, _ := ParseCGMiner([]byte(`{"STATS":[{"MM ID0":"PVT_T0[60 64 68]"}]}`))
	value(t, r.Boards[0].Chip.AverageC, 64)
	if r.Boards[0].Chip.AverageKind != "sensor_mean" {
		t.Fatal(r.Boards)
	}
}
func TestByteonPublishedSensorSchemaHonorsStatus(t *testing.T) {
	r, err := ParseByteon([]byte(`{"miner":{"hr_realtime":95000,"chains":[{"id":0,"chip_temp_sens":[{"status":"measure","temp":60},{"status":"error","temp":120},{"status":"measure","temp":64}],"pcb_temp_sens":[{"status":"measure","temp":40},{"status":"measure","temp":42}]},{"id":2,"chip_temp_sens":[{"status":"measure","temp":70},{"status":"init","temp":0}]}]}}`), byteonInfo)
	if err != nil || len(r.Boards) != 2 {
		t.Fatal(r, err)
	}
	value(t, r.Boards[0].Chip.AverageC, 62)
	value(t, r.Boards[0].PCB.AverageC, 41)
	if r.Boards[0].Chip.SensorsC[1] != nil || r.Boards[1].Chip.AverageC != nil {
		t.Fatal(r.Boards)
	}
}
func TestTemperatureMergeDoesNotConflateProtocolBoardIDs(t *testing.T) {
	a, _ := ParseAntminer([]byte(`{"STATS":[{"chain":[{"index":0,"temp_chip":[60,64]}]}]}`))
	b, _ := ParseByteon([]byte(`{"miner":{"hr_realtime":95000,"chains":[{"id":0,"chip_temp_sens":[{"status":"measure","temp":70},{"status":"measure","temp":74}]}]}}`), byteonInfo)
	merged := merge(a, b)
	if len(merged.Boards) != 2 || merged.Boards[0].ID == merged.Boards[1].ID {
		t.Fatal(merged.Boards)
	}
	value(t, merged.Boards[0].Chip.AverageC, 62)
	value(t, merged.Boards[1].Chip.AverageC, 72)
	if len(merge(a, a).Boards) != 1 {
		t.Fatal("same endpoint duplicated")
	}
}
func TestTemperatureDiagnosticRetainsIDsAndAvalonChipArrays(t *testing.T) {
	data := redactTelemetry([]byte(`{"msg":{"edevs":[{"slot":2,"id":0,"chip-temp-avg":65,"serial":"SECRET"}]},"STATS":[{"MM ID0":"MTavg[65 66] PVT_T0[60 70] User[SECRET]"}]}`))
	b, _ := json.Marshal(data)
	for _, want := range []string{"slot", "65", "PVT_T0", "MTavg"} {
		if !strings.Contains(string(b), want) {
			t.Fatal(want, string(b))
		}
	}
	if strings.Contains(string(b), "SECRET") {
		t.Fatal("secret leaked")
	}
}

func TestAntminerHyphenBoardTemperatures(t *testing.T) {
	for _, raw := range []string{
		`{"STATS":[{"chain":[{"index":0,"temp_chip":"60-62-64-66"},{"index":1,"temp_chip":"70-72-74-76"},{"index":2,"temp_chip":"50-52-54-56"}]}]}`,
		`{"STATS":[{"temp2_0":"60-62-64-66","temp2_1":"70-72-74-76","temp2_2":"50-52-54-56"}]}`,
	} {
		r, e := ParseAntminer([]byte(raw))
		if e != nil || len(r.Boards) != 3 {
			t.Fatalf("%v %+v", e, r)
		}
		for i, b := range r.Boards {
			value(t, b.Chip.AverageC, []float64{63, 73, 53}[i])
			if b.Chip.AverageKind != "sensor_mean" || len(b.Chip.SensorsC) != 4 {
				t.Fatal(b)
			}
		}
	}
	for _, raw := range []string{"-10", "60--64", "60-unknown-64", "60"} {
		s := antminerSamplesSeries(raw, "test")
		if s.AverageC != nil {
			t.Fatalf("invented mean for %q: %+v", raw, s)
		}
	}
	s := antminerSamplesSeries("60-255-64-66", "test")
	if len(s.SensorsC) != 4 || s.SensorsC[1] != nil {
		t.Fatal(s)
	}
}

func TestAntminerHyphenDiagnostic(t *testing.T) {
	out := redactTelemetry([]byte(`{"STATS":[{"temp2_0":"60-62-64-66","temp2_1":"account-secret","temp2_2":"-10"}]}`))
	stats := out["STATS"].([]any)[0].(map[string]any)
	if _, ok := stats["temp2_1"]; ok {
		t.Fatal(stats)
	}
	values, ok := stats["temp2_0"].([]*float64)
	if !ok || len(values) != 4 {
		t.Fatal(stats)
	}
	value(t, values[0], 60)
}
