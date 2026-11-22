package miner

import "testing"

func TestAvalonLogNewestPS(t *testing.T) {
	r, e := ParseAvalonLog([]byte("PS[0 1217 1279 221 1280 8800]\nPS[0 1217 1279 221 1280 9100]"))
	if e != nil || r.PowerW == nil || *r.PowerW != 9100 {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestAvalonLogHistoryNotSum(t *testing.T) {
	r, e := ParseAvalonLog([]byte("HS_RCD0[Tenv:23 P_P:8801] HS_RCD1[Tenv:23 P_P:8810]"))
	if e != nil || r.PowerW == nil || *r.PowerW != 8810 {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestAvalonLogRejectUnknownPS(t *testing.T) {
	r, e := ParseAvalonLog([]byte("PS[]"))
	if e == nil || r.PowerW != nil {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestAvalonLogZeroValid(t *testing.T) {
	r, e := ParseAvalonLog([]byte("PS[0 1217 1279 0 1280 0]"))
	if e != nil || r.PowerW == nil || *r.PowerW != 0 {
		t.Fatalf("%+v %v", r, e)
	}
}
