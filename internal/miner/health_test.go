package miner

import (
	"encoding/json"
	"strings"
	"testing"
)

func healthFixture(t *testing.T, payload string) map[string]any {
	t.Helper()
	m, err := decode([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestHealthAPIRepliesAreNotMiningState(t *testing.T) {
	for _, payload := range []string{
		`{}`,
		`{"STATUS":[{"STATUS":"E","Code":14,"Msg":"invalid command"}]}`,
		`{"STATUS":{"STATUS":"S","Msg":"summary"},"SUMMARY":[{"GHS 5s":0}]}`,
		`{"code":-4,"msg":"No permission"}`,
		`{"code":0,"msg":"ok"}`,
		`{"error":"fan error","status":"failure","reason":"overheat"}`,
	} {
		m := healthFixture(t, payload)
		for _, parse := range []func(map[string]any) Reading{healthCGMiner, healthWhatsminerV3, healthByteon} {
			r := parse(m)
			if r.StatusKnown || len(r.Faults) != 0 {
				t.Fatalf("API acknowledgement became mining health: %s -> %+v", payload, r)
			}
		}
	}
}

func TestHealthAntminerCurrentStatusAndSleep(t *testing.T) {
	r := healthCGMiner(healthFixture(t, `{
		"STATUS":{"STATUS":"S"},
		"SUMMARY":[{"status":[
			{"type":"rate","status":"s","code":0,"msg":"old fan error"},
			{"type":"fans","status":"e","code":0,"msg":"fan lost"},
			{"type":"temp","status":"e","code":"T100","msg":"overheated"}
		]}],"STATS":[{"miner-mode":1}]
	}`))
	if !r.StatusKnown || len(r.Faults) != 3 {
		t.Fatalf("missing current states: %+v", r)
	}
	if r.Faults[0].Code != "fan" || !strings.Contains(r.Faults[0].Evidence, "code=0") || r.Faults[1].Code != "overheat" || r.Faults[2].Code != "paused" {
		t.Fatalf("wrong categories or valid zero code lost: %+v", r.Faults)
	}
	for _, fault := range r.Faults {
		if fault.Confidence != "reported" {
			t.Fatalf("structured current observation lost provenance: %+v", fault)
		}
	}
}

func TestHealthAntminerNormalAndUnknownStatus(t *testing.T) {
	for _, tc := range []struct {
		payload string
		known   bool
	}{
		{`{"SUMMARY":[{"status":[{"type":"fans","status":"s","code":0}]}]}`, true},
		{`{"STATS":[{"miner-mode":0}]}`, true},
		{`{"STATS":[{"miner-mode":"2"}]}`, true},
		{`{"STATS":[{"miner-mode":3}]}`, true},
		{`{"STATS":[{"miner-mode":9}]}`, false},
		{`{"STATS":[{"miner-mode":1.5}]}`, false},
		{`{"SUMMARY":[{"status":[{"type":"future-kind","status":"e","code":1}]}]}`, false},
		{`{"SUMMARY":[{"status":[{"type":"fans","status":"unknown","code":1}]}]}`, false},
	} {
		r := healthCGMiner(healthFixture(t, tc.payload))
		if r.StatusKnown != tc.known || len(r.Faults) != 0 {
			t.Fatalf("unexpected health for %s: %+v", tc.payload, r)
		}
	}
}

func TestHealthPoolsRequireAllClearlyDead(t *testing.T) {
	for _, tc := range []struct {
		name, pools string
		wantFault   bool
	}{
		{"primary alive backup dead", `[{"status":"Alive"},{"status":"Dead"}]`, false},
		{"primary dead backup alive", `[{"status":"Dead"},{"status":"Alive"}]`, false},
		{"all explicitly dead", `[{"status":"dead"},{"status":"Dead"}]`, true},
		{"legacy capitalization", `[{"Status":"Dead"},{"Status":"Dead"}]`, true},
		{"unknown peer", `[{"status":"Dead"},{"status":"unknown"}]`, false},
		{"missing peer status", `[{"status":"Dead"},{}]`, false},
		{"malformed peer", `[{"status":"Dead"},"bad row"]`, false},
		{"unconfigured empty slot", `[{"url":"","status":"Dead"}]`, false},
		{"empty", `[]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cg := healthCGMiner(healthFixture(t, `{"POOLS":`+tc.pools+`}`))
			whats := healthWhatsminerV3(healthFixture(t, `{"pools":`+tc.pools+`}`))
			for _, r := range []Reading{cg, whats} {
				if (len(r.Faults) > 0) != tc.wantFault {
					t.Fatalf("pool inference false positive/missing: %+v", r)
				}
				if tc.wantFault && (r.Faults[0].Confidence != "possible" || r.Faults[0].Code != "pool") {
					t.Fatalf("pool connection is evidence, not proved hardware causality: %+v", r)
				}
			}
		})
	}
}

func TestHealthWhatsminerWorkingAndTimestampedCodes(t *testing.T) {
	r := healthWhatsminerV3(healthFixture(t, `{"miner":{"working":"false"},"error-code":[
		{"531":"2025-03-12 14:52:35","reason":"Slot1 not found."},
		{"532":"2025-03-12T14:52:35+08:00","reason":"private account admin-wallet"}
	]}`))
	if !r.StatusKnown || len(r.Faults) != 3 || r.Faults[0].Code != "not-working" {
		t.Fatalf("missing working/error information: %+v", r)
	}
	for _, fault := range r.Faults[1:] {
		if fault.Confidence != "possible" || !strings.Contains(fault.Evidence, "2025-03-12") || !strings.Contains(fault.Evidence, "code="+fault.Code) {
			t.Fatalf("retained errors incorrectly presented as certain/current: %+v", fault)
		}
	}
	if !strings.Contains(r.Faults[1].Message, "插槽 1") || strings.Contains(r.Faults[2].Message, "admin-wallet") {
		t.Fatalf("safe reason classification failed: %+v", r.Faults)
	}
	for _, payload := range []string{`{"miner":{"working":true}}`, `{"error-code":[]}`} {
		r := healthWhatsminerV3(healthFixture(t, payload))
		if !r.StatusKnown || len(r.Faults) != 0 {
			t.Fatalf("explicit normal state lost: %+v", r)
		}
	}
}

func TestHealthWhatsminerMalformedCodeDoesNotBecomeFault(t *testing.T) {
	for _, payload := range []string{
		`{"miner":{"working":"no"}}`,
		`{"error-code":"permission denied"}`,
		`{"error-code":[{"531":"wallet-account","reason":"Slot1 not found."}]}`,
		`{"error-code":[{"wallet-account":"2025-03-12 14:52:35","reason":"fan error"}]}`,
		`{"error-code":[{"531":"2025-02-30 14:52:35"}]}`,
		`{"error-code":[{"-4":"2025-03-12 14:52:35"}]}`,
	} {
		r := healthWhatsminerV3(healthFixture(t, payload))
		if r.StatusKnown || len(r.Faults) != 0 {
			t.Fatalf("malformed status trusted: %s -> %+v", payload, r)
		}
	}
}

func TestHealthAvalonRawCodesAndHistoryIsolation(t *testing.T) {
	r := healthCGMiner(healthFixture(t, `{"STATS":[{"MM ID0":"SYSTEMSTATU[Work: In Idle, Hash Board: 0 ] ECHU[512 513 128 65535] ECMM[1] BOOTBY[2] NETFAIL[10 20]"}]}`))
	if !r.StatusKnown || len(r.Faults) != 4 {
		t.Fatalf("missing Avalon current fields: %+v", r)
	}
	if r.Faults[0].Evidence != "Work=In Idle; Hash Board=0" || r.Faults[1].Evidence != "ECHU[512 513 128 65535]" || r.Faults[2].Evidence != "ECMM[1]" {
		t.Fatalf("safe original codes/state lost: %+v", r.Faults)
	}
	for _, f := range r.Faults[1:3] {
		if f.Confidence != "possible" || strings.Contains(f.Message, "过热") || strings.Contains(f.Message, "损坏") {
			t.Fatalf("legacy code meaning generalized to unknown model: %+v", f)
		}
	}
	if f := r.Faults[3]; f.Code != "avalon-bootby" || f.Evidence != "BOOTBY[2]" || f.Confidence != "historical" {
		t.Fatalf("restart history not classified separately: %+v", f)
	}
	r = healthCGMiner(healthFixture(t, `{"STATS":[{"MM ID0":"BOOTBY[2] NETFAIL[10 20] HW[100]"}]}`))
	if r.StatusKnown || hasObservation(r) || len(r.Faults) != 1 {
		t.Fatalf("restart history changed current state: %+v", r)
	}
	for _, payload := range []string{
		`{"STATS":[{"MM ID0":"BOOTBY[0x11.token40z] NETFAIL[10 20] HW[100]"}]}`,
		`{"STATS":[{"MM ID0":"SYSTEMSTATU[Work: secret-wallet, Hash Board: 0] ECHU[secret-account] ECMM[secret-token]"}]}`,
		`{"STATS":[{"MM ID0":"ECHU[] ECMM[-1]"}]}`,
	} {
		r := healthCGMiner(healthFixture(t, payload))
		if r.StatusKnown || len(r.Faults) != 0 {
			t.Fatalf("history/malformed code became current state: %+v", r)
		}
	}
	for _, payload := range []string{
		`{"STATS":[{"MM ID0":"SYSTEMSTATU[Work: In Work, Hash Board: 3] ECHU[0 512] ECMM[0]"}]}`,
		`{"STATS":[{"MM ID0":"ECHU[000 0512] ECMM[00]"}]}`,
	} {
		r := healthCGMiner(healthFixture(t, payload))
		if !r.StatusKnown || len(r.Faults) != 0 {
			t.Fatalf("benign Avalon status became fault: %+v", r)
		}
	}
}

func TestHealthByteonStatesAndBoardIdentity(t *testing.T) {
	r := healthByteon(healthFixture(t, `{
		"miner_status":{"miner_state":"auto-tuning"},
		"chains":[
			{"id":0,"status":{"state":"failure","description":"fan lost"}},
			{"id":2,"status":{"state":"disconnected"}},
			{"id":7,"status":{"state":"disabled"}},
			{"id":8,"status":{"state":"mining","description":"previous failure: overheat"}}
		]
	}`))
	if !r.StatusKnown || len(r.Faults) != 4 || r.Faults[0].Code != "tuning" {
		t.Fatalf("missing Byteon states: %+v", r)
	}
	for i, id := range []string{"0", "2", "7"} {
		if !strings.Contains(r.Faults[i+1].Evidence, "chain="+id) {
			t.Fatalf("original board identity lost: %+v", r.Faults)
		}
	}
	for _, state := range []string{"initializing", "starting", "restarting", "shutting-down", "stopped", "failure"} {
		r := healthByteon(healthFixture(t, `{"miner_status":{"miner_state":"`+state+`"}}`))
		if !r.StatusKnown || len(r.Faults) != 1 || r.Faults[0].Confidence != "reported" {
			t.Fatalf("documented state lost: %s -> %+v", state, r)
		}
	}
	for _, payload := range []string{
		`{"miner_status":{"miner_state":"mining","description":"old fan error"}}`,
		`{"chains":[{"id":0,"status":{"state":"unknown"}}]}`,
	} {
		r := healthByteon(healthFixture(t, payload))
		if !r.StatusKnown || len(r.Faults) != 0 {
			t.Fatalf("normal/unknown state became fault: %+v", r)
		}
	}
}

func TestHealthDescriptionsNeverCopyPrivateOrNegatedText(t *testing.T) {
	for _, description := range []string{
		"fan error at stratum+tcp://pool.private.invalid for wallet-user password=secret",
		"No fan errors",
		"Previous overheat recovered",
		"Slot1 not found. token=secret",
		strings.Repeat("sensitive", 100),
	} {
		encoded, _ := json.Marshal(description)
		r := healthByteon(healthFixture(t, `{"miner_status":{"miner_state":"failure","description":`+string(encoded)+`}}`))
		if len(r.Faults) != 1 || r.Faults[0].Code != "firmware-failure" {
			t.Fatalf("unsafe/negated text inferred a component failure: %+v", r)
		}
		out, _ := json.Marshal(r)
		if strings.Contains(string(out), description) || strings.Contains(string(out), "secret") || strings.Contains(string(out), "wallet") {
			t.Fatalf("private description retained: %s", out)
		}
	}
	r := healthCGMiner(healthFixture(t, `{"POOLS":[{"URL":"stratum+tcp://pool.private.invalid","User":"private-wallet","Pass":"private-password","Status":"Dead"}]}`))
	data, _ := json.Marshal(r)
	if strings.Contains(string(data), "private") {
		t.Fatalf("pool credentials retained: %s", data)
	}
}

func TestHealthMalformedNestedStateDoesNotClaimKnown(t *testing.T) {
	for _, payload := range []string{
		`{"miner_status":{"miner_state":"future-state","description":"fan error"}}`,
		`{"chains":[{"id":0,"status":{"state":"future-state","description":"fan error"}}]}`,
		`{"miner_status":"failure","chains":[{"status":"failure"}]}`,
	} {
		r := healthByteon(healthFixture(t, payload))
		if r.StatusKnown || len(r.Faults) != 0 {
			t.Fatalf("unsupported schema became known: %+v", r)
		}
	}
}
