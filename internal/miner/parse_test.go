package miner

import (
	"math"
	"testing"
)

func value(t *testing.T, p *float64, want float64) {
	t.Helper()
	if p == nil || math.Abs(*p-want) > 1e-8 {
		t.Fatalf("value %v, want %v", p, want)
	}
}
func TestAntminerModernUnitsAndZero(t *testing.T) {
	r, err := ParseAntminer([]byte(`{"STATS":[{"rate_unit":"GH/s","rate_5s":0,"rate_avg":95400,"chain":[{"temp_chip":[58,63,62]}]}],"miner_type":"Antminer S19"}`))
	if err != nil {
		t.Fatal(err)
	}
	value(t, r.RealtimeTH, 0)
	value(t, r.AverageTH, 95.4)
	value(t, r.TempC, 63)
	if r.PowerW != nil {
		t.Fatal("must not fabricate rated power")
	}
	if r.Brand != "Antminer" || r.Model != "Antminer S19" {
		t.Fatal(r)
	}
}
func TestCGMinerUnitsAndStrings(t *testing.T) {
	r, e := ParseCGMiner([]byte(`{"SUMMARY":[{"MHS 5s":"103000000.0","MHS av":100000000}],"STATS":[{"Type":"Antminer S19","temp2_1":"65,69,67","temp2_2":"62,71"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.RealtimeTH, 103)
	value(t, r.AverageTH, 100)
	value(t, r.TempC, 71)
}
func TestAntminerLegacyStatus(t *testing.T) {
	r, e := ParseAntminer([]byte(`{"summary":[{"GHS5s":"98000","GHSav":"97000"}],"stats":[{"temp2_1":"66,68"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.RealtimeTH, 98)
	value(t, r.AverageTH, 97)
	value(t, r.TempC, 68)
}
func TestUnknownUnitNotGuessed(t *testing.T) {
	r, e := ParseAntminer([]byte(`{"STATS":[{"rate_unit":"unknown","rate_5s":120000,"rate_avg":110000}]}`))
	if e != nil {
		t.Fatal(e)
	}
	if hasData(r) {
		t.Fatal("unknown units must stay unknown")
	}
}
func TestWhatsminerLegacy(t *testing.T) {
	r, e := ParseCGMiner([]byte(`{"SUMMARY":[{"MHS 5s":390000000,"MHS av":389000000,"HS RT":391000000,"Power":7210,"Temperature":72}]}`))
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.RealtimeTH, 391)
	value(t, r.AverageTH, 389)
	value(t, r.PowerW, 7210)
	if r.Brand != "Whatsminer" {
		t.Fatal(r)
	}
}
func TestAvalonModulesAndPowerProvenance(t *testing.T) {
	r, e := ParseCGMiner([]byte(`{"STATS":[{"MM ID0":"Ver[1566HA-build] GHSmm[230000] GHSavg[226000] TMax[75] Temp[28] PS[0 1200 1300 250 1300 3250]","MM ID1":"GHSmm[10000] GHSavg[9000] TMax[76] PS[0 1200 1300 10 1300 130]"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.RealtimeTH, 240)
	value(t, r.AverageTH, 235)
	value(t, r.TempC, 76)
	value(t, r.PowerW, 3380)
	if r.PowerSource != "矿机日志 PS 末项（W）" {
		t.Fatal(r.PowerSource)
	}
	if r.Brand != "Avalon" {
		t.Fatal(r)
	}
}
func TestAvalonPSVariableLengthLastValue(t *testing.T) {
	r, e := ParseCGMiner([]byte(`{"STATS":[{"MM ID0":"GHSmm[230000] PS[0 1 2 3 999 5 6]"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.PowerW, 6)
}
func TestAvalonLegacyPipe(t *testing.T) {
	r, e := ParseCGMiner([]byte("STATUS=S,Code=70|STATS=0,ID=AVA100,MM ID0=Ver[1566HA-build] SYSTEMSTATU[Work: In Work, Hash Board: 3] GHSmm[230000] GHSavg[225000] TMax[73] PS[0 1200 1300 250 1300 3250],MM Count=1|"))
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.RealtimeTH, 230)
	value(t, r.PowerW, 3250)
}
func TestWhatsminerV3(t *testing.T) {
	r, e := ParseWhatsminerV3([]byte(`{"code":0,"msg":{"summary":{"hash-realtime":390.5,"hash-average":389.3,"power-realtime":7215,"chip-temp-max":76.3,"environment-temperature":29},"edevs":[{"chip-temp-max":78.2}]}}`), []byte(`{"code":0,"msg":{"miner":{"type":"M63S"},"power":{"pin":7220}}}`))
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.RealtimeTH, 390.5)
	value(t, r.AverageTH, 389.3)
	value(t, r.TempC, 78.2)
	value(t, r.PowerW, 7215)
	if r.InletC != nil {
		t.Fatal("environment temperature must not become water inlet temperature")
	}
	if r.Model != "M63S" {
		t.Fatal(r)
	}
}

func TestAmbientDoesNotBecomeWaterTemperature(t *testing.T) {
	r, e := ParseAntminer([]byte(`{"STATS":[{"rate_unit":"GH/s","rate_5s":95000,"ambient_temp":31}]}`))
	if e != nil {
		t.Fatal(e)
	}
	if r.InletC != nil {
		t.Fatal("ambient mapped to inlet water")
	}
	a, e := ParseCGMiner([]byte(`{"STATS":[{"MM ID0":"GHSmm[230000] Temp[27] TMax[70]"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	if a.InletC != nil {
		t.Fatal("Avalon Temp mapped to inlet water")
	}
}
func TestWhatsminerV3RejectsErrors(t *testing.T) {
	_, e := ParseWhatsminerV3([]byte(`{"code":-4,"msg":"no permission"}`), nil)
	if e == nil {
		t.Fatal("expected error")
	}
}
func TestWhatsminerV3DoesNotUsePowerLimit(t *testing.T) {
	r, e := ParseWhatsminerV3([]byte(`{"code":0,"msg":{"summary":{"hash-1min":0,"hash-average":0,"power-limit":8000}}}`), nil)
	if e != nil {
		t.Fatal(e)
	}
	value(t, r.RealtimeTH, 0)
	if r.PowerW != nil {
		t.Fatal("limit is not actual power")
	}
}
func TestNegativeHashrateInvalid(t *testing.T) {
	r, e := ParseCGMiner([]byte(`{"SUMMARY":[{"MHS 5s":-1,"MHS av":0}]}`))
	if e != nil {
		t.Fatal(e)
	}
	if r.RealtimeTH != nil {
		t.Fatal("negative hashrate accepted")
	}
	value(t, r.AverageTH, 0)
}
