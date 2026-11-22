package miner

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestWorkerNamesAcrossProtocols(t *testing.T) {
	a, e := ParseCGMiner([]byte(`{"POOLS":[{"User":"farm.a","Pass":"private"},{"User":"farm.a"},{"User":"farm.b"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	b, e := ParseWhatsminerV3([]byte(`{"code":0,"msg":{"pools":[{"user":"farm.a"},{"user":"farm.b"}]}}`), nil)
	if e != nil {
		t.Fatal(e)
	}
	c := avalonConfigWorkers([]byte(`CGConfCallback({"worker1":"farm.a","worker2":"farm.a","worker3":"farm.b","passwd1":"private"});`))
	for _, r := range []Reading{a, b, c} {
		if !r.WorkersKnown || !reflect.DeepEqual(r.WorkerNames, []string{"farm.a", "farm.b"}) {
			t.Fatal(r.WorkerNames)
		}
		raw, _ := json.Marshal(r)
		if strings.Contains(string(raw), "private") {
			t.Fatal(string(raw))
		}
	}
	if workerName("injected\nname") != "" {
		t.Fatal("control characters accepted")
	}
	if !poolWorkers([]any{}).WorkersKnown || poolWorkers(nil).WorkersKnown {
		t.Fatal("missing vs empty")
	}
}
