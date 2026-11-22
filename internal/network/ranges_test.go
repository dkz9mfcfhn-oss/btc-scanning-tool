package network

import (
	"reflect"
	"testing"
)

func TestExpandRanges(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"192.168.1.3-5,192.168.1.4; 192.168.1.2\n192.168.1.10", []string{"192.168.1.2", "192.168.1.3", "192.168.1.4", "192.168.1.5", "192.168.1.10"}},
		{"10.0.0.10/30", []string{"10.0.0.9", "10.0.0.10"}},
		{"10.0.0.10/31", []string{"10.0.0.10", "10.0.0.11"}},
		{"172.16.1.5/32", []string{"172.16.1.5"}},
		{"192.168.1.254-192.168.2.1", []string{"192.168.1.254", "192.168.1.255", "192.168.2.0", "192.168.2.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ExpandRanges(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestRejectUnsafeOrInvalidRanges(t *testing.T) {
	for _, input := range []string{"", "8.8.8.8", "127.0.0.1", "169.254.1.1", "::1", "::ffff:192.168.1.1", "224.0.0.1", "192.168.1.250-2", "192.168.1.1-256", "192.168.1.1--2", "10.0.0.0/8", "0.0.0.0/0", "192.168.1.1,8.8.8.8", "172.31.255.250-172.32.0.1", "192.168.1.999", "10.0.0.1/33", "10.0.0.0-10.0.16.0", "10.0.0.0/20,10.1.0.0/20"} {
		t.Run(input, func(t *testing.T) {
			if got, err := ExpandRanges(input); err == nil {
				t.Fatalf("accepted %s (%d targets)", input, len(got))
			}
		})
	}
}

func TestLimitsAndDedup(t *testing.T) {
	got, err := ExpandRanges("10.0.0.0-10.0.15.255,10.0.0.1")
	if err != nil || len(got) != MaxTargets {
		t.Fatalf("expected %d got %d err %v", MaxTargets, len(got), err)
	}
	got, err = ExpandRanges("10.0.0.0/20")
	if err != nil || len(got) != 4094 {
		t.Fatalf("CIDR host limit got %d err %v", len(got), err)
	}
}
