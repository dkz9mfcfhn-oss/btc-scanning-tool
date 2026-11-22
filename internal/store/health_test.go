package store

import (
	"minerwatch/internal/telemetry"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestHealthPersistenceAndDeepCopy(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	faults := []telemetry.Fault{{Code: "starting", Message: "矿机正在启动", Source: "summary/status", Evidence: "state=starting", Confidence: "reported"}, {Code: "pool", Message: "矿池未连接", Source: "log/recent", Evidence: "no active pools", Confidence: "log"}}
	r := Record{IP: "192.168.1.25", Sample: Sample{At: now, Online: true, RealtimeTH: ptr(0), Health: telemetry.AssessHashrate(ptr(0), true, faults, "")}}
	want := telemetry.CloneHealth(r.Sample.Health)
	if err := s.Append([]Record{r}); err != nil {
		t.Fatal(err)
	}
	r.Sample.Health.Evidence[0].Message = "modified input"
	got := s.History(r.IP, now.Add(-time.Minute))[0]
	if !reflect.DeepEqual(got.Health, want) {
		t.Fatal("input health evidence aliases stored history")
	}
	got.Health.Evidence[0].Evidence = "modified History result"
	all := s.All(now.Add(-time.Minute))
	all[0].Sample.Health.Evidence[1].Confidence = "reported"
	if !reflect.DeepEqual(s.History(r.IP, now.Add(-time.Minute))[0].Health, want) {
		t.Fatal("returned health evidence aliases stored history")
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.History(r.IP, now.Add(-time.Minute)); len(got) != 1 || !reflect.DeepEqual(got[0].Health, want) {
		t.Fatalf("health did not survive restart: %+v", got)
	}
}

func TestLegacyHistoryWithoutHealth(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	legacy := `{"ip":"192.168.1.25","sample":{"at":"` + now.Format(time.RFC3339Nano) + `","online":true,"realtimeTH":0,"tempC":70}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, now.Format("2006-01-02")+".jsonl"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := s.History("192.168.1.25", now.Add(-time.Minute))
	if len(got) != 1 || got[0].RealtimeTH == nil || *got[0].RealtimeTH != 0 || got[0].Health.State != "" || len(got[0].Health.Evidence) != 0 {
		t.Fatalf("legacy record must retain its data without fabricated health evidence: %+v", got)
	}
}
