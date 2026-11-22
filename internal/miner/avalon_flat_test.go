package miner

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Derived from the supplied 1566HA firmware log; identifying fields are synthetic.
const flat1566 = `Ver[1566HA-512-25111202_7ec0a2b] SYSTEMSTATU[Work: In Work, Hash Board: 4] DNA[private-test-marker] NETFAIL[123 456] ITemp[54] HBITemp[65 64 61 61] HBOTemp[66 66 62 64] TMax[82] TAvg[65] GHSspd[569733.00] GHSmm[584381.00] GHSavg[567594.80] MGHS[140447.29 145603.45 137219.30 144324.76] MTmax[77 82 75 79] MTavg[66 68 63 64] ECHU[0 0 0 0] ECMM[0]`

func Test1566FlatStats(t *testing.T) {
	encoded, _ := json.Marshal(map[string]any{"STATS": flat1566})
	legacy, _ := json.Marshal(map[string]any{"STATS": []any{map[string]any{"MM ID0": flat1566}}})
	for _, data := range []string{"'STATS':{" + flat1566, "'STATS':{" + flat1566 + "}", string(encoded), string(legacy)} {
		r, err := ParseCGMiner([]byte(data))
		if err != nil || len(r.Boards) != 4 || r.Brand != "Avalon" || !hasBoardMeans(r) {
			t.Fatalf("%v %+v", err, r)
		}
		for i, b := range r.Boards {
			value(t, b.Chip.AverageC, []float64{66, 68, 63, 64}[i])
			value(t, b.Chip.MaxC, []float64{77, 82, 75, 79}[i])
			if b.Chip.AverageKind != "reported" {
				t.Fatal(b)
			}
			if data != string(legacy) && (b.ID != fmt.Sprintf("avalon:stats:board:%d", i) || strings.Contains(b.Name, "模块")) {
				t.Fatal(b)
			}
		}
		value(t, r.AverageTH, 567.5948)
		sanitized, _ := json.Marshal(redactTelemetry([]byte(data)))
		if strings.Contains(string(sanitized), "private-test-marker") || strings.Contains(string(sanitized), "NETFAIL") || !strings.Contains(string(sanitized), "MTavg") {
			t.Fatal(string(sanitized))
		}
	}
}

func TestUnrelatedBracketLogRejected(t *testing.T) {
	if _, err := ParseCGMiner([]byte(`'STATS':{Message[hello] MTavg[66 68]`)); err == nil {
		t.Fatal("accepted unrelated log")
	}
}
