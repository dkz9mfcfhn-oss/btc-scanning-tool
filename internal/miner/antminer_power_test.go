package miner

import "testing"

func TestAntminerReportedPowerLayouts(t *testing.T) {
	for _, body := range []string{`{"STATS":[{"watt":3210}]}`, `{"SUMMARY":[{"power_actual":3210}]}`, `{"power":{"actual":3210}}`, `{"power_w":3210}`} {
		r, e := ParseAntminer([]byte(body))
		if e != nil || r.PowerW == nil || *r.PowerW != 3210 {
			t.Fatalf("%s %+v %v", body, r, e)
		}
	}
}
func TestAntminerNoPowerRemainsMissing(t *testing.T) {
	r, e := ParseAntminer([]byte(`{"STATS":[{"rate_5s":95,"rate_unit":"TH/s","power_limit":3250,"rated_power":3250}]}`))
	if e != nil || r.PowerW != nil {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestAntminerZeroPower(t *testing.T) {
	r, e := ParseAntminer([]byte(`{"STATS":[{"watt":0}]}`))
	if e != nil || r.PowerW == nil || *r.PowerW != 0 {
		t.Fatal(r, e)
	}
}
