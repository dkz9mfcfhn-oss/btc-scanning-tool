package miner

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func Test1566UserDiagnosticTemperatureReplay(t *testing.T) {
	data, e := os.ReadFile("testdata/avalon1566-diagnostic-temperature.json")
	if e != nil {
		t.Fatal(e)
	}
	var r Reading
	if e = json.Unmarshal(data, &r); e != nil {
		t.Fatal(e)
	}
	if got := originalValues(r); !reflect.DeepEqual(got, []float64{0}) {
		t.Fatal(got)
	}
	correctAvalonOverview(&r)
	value(t, r.TempC, 81)
	if got := originalValues(r); !reflect.DeepEqual(got, []float64{81}) {
		t.Fatal(got)
	}
	if len(r.Boards) != 0 || hasBoardMeans(r) {
		t.Fatal("invented board means from overview")
	}
}
func TestAvalonAlternateModuleLabelsKeepTemperatureAndRedaction(t *testing.T) {
	for _, key := range []string{"MM ID0:Status", " MM ID0 ", "MM ID0 (module)", "STATS"} {
		raw, _ := json.Marshal(map[string]any{"STATS": []any{map[string]any{key: flat1566}}})
		r, e := ParseCGMiner(raw)
		if e != nil || len(r.Boards) != 4 {
			t.Fatalf("%q %v %+v", key, e, r)
		}
		if got := originalValues(r); !reflect.DeepEqual(got, []float64{66, 68, 63, 64}) {
			t.Fatal(key, got)
		}
		value(t, r.RealtimeTH, 584.381)
		safe, _ := json.Marshal(redactTelemetry(raw))
		if !strings.Contains(string(safe), "MTavg") || strings.Contains(string(safe), "private-test-marker") {
			t.Fatal(string(safe))
		}
	}
}
func TestAvalonReportedZeroWithoutIndependentTemperatureStaysZero(t *testing.T) {
	r := Reading{Brand: "Avalon", TempC: ptr(0)}
	setBTCTemperatures(&r, []*float64{ptr(0)}, "DEVS.Temperature")
	correctAvalonOverview(&r)
	if got := originalValues(r); !reflect.DeepEqual(got, []float64{0}) {
		t.Fatal(got)
	}
}
