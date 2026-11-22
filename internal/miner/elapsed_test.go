package miner

import "testing"

func TestElapsedFields(t *testing.T) {
	tests := []struct {
		name, payload string
		parse         func([]byte) (Reading, error)
		want          float64
	}{
		{"stats counter", `{"STATS":[{"Elapsed":0},{"Elapsed":86461,"rate_unit":"TH/s","rate_5s":95}]}`, ParseCGMiner, 86461},
		{"legacy summary", "SUMMARY,Elapsed=91,MHS 5s=1000|", ParseCGMiner, 91},
		{"antminer status", `{"summary":{"elapsed":"75","ghs5s":90000}}`, ParseAntminer, 75},
		{"Avalon module", `{"STATS":[{"MM ID0":"Elapsed[8760] GHSmm[90]"}]}`, ParseCGMiner, 8760},
		{"Avalon FMS", "Elapsed[4321] MTavg[60 65] TMax[66]", parseAvalonFMS, 4321},
		{"Whatsminer v3", `{"code":0,"msg":{"summary":{"elapsed":1074,"bootup-time":1175,"hash-realtime":90}}}`, func(b []byte) (Reading, error) { return ParseWhatsminerV3(b, nil) }, 1074},
		{"restart", `{"SUMMARY":[{"Elapsed":0}]}`, ParseCGMiner, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := tc.parse([]byte(tc.payload))
			if err != nil || r.ElapsedSeconds == nil || *r.ElapsedSeconds != tc.want {
				t.Fatalf("elapsed = %v, error = %v; want %v", r.ElapsedSeconds, err, tc.want)
			}
		})
	}
	for _, payload := range []string{`{"SUMMARY":[{}]}`, `{"SUMMARY":[{"Elapsed":-1}]}`, `{"SUMMARY":[{"Elapsed":1e30}]}`} {
		r, err := ParseCGMiner([]byte(payload))
		if err != nil || r.ElapsedSeconds != nil {
			t.Fatalf("expected unknown runtime: %s", payload)
		}
	}
}
