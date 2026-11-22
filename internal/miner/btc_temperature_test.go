package miner

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func originalValues(r Reading) []float64 {
	var v []float64
	for _, s := range r.Temperatures {
		if strings.HasPrefix(s.Source, btcTemperaturePrefix) && s.ValueC != nil {
			v = append(v, *s.ValueC)
		}
	}
	return v
}
func TestBTCOriginalSelectionAndOverwrite(t *testing.T) {
	a, e := ParseCGMiner([]byte(`{"STATS":[{"Type":"Antminer S19 XP+ Hyd"},{"temp0":90,"temp1":61,"temp2":62,"temp3":63,"temp_chip4":"80-81"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	v := originalValues(a)
	if len(v) != 3 || v[0] != 61 || v[2] != 63 {
		t.Fatal(v)
	}
	b, e := ParseAntminer([]byte(`{"devs":[{"freq":"500,temp1=64,temp2=65,temp3=66"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	c := merge(a, b)
	v = originalValues(c)
	if len(v) != 3 || v[0] != 64 || v[2] != 66 {
		t.Fatal(v)
	}
	// Source input is not modified while merging.
	if originalValues(a)[0] != 61 {
		t.Fatal("mutated original")
	}
}
func TestBTCDEVSAndGaps(t *testing.T) {
	r, _ := ParseCGMiner([]byte(`{"DEVS":[{"Temperature":-273},{"Temperature":61},{"Temperature":62},{"Temperature":63}]}`))
	v := originalValues(r)
	if len(v) != 3 {
		t.Fatal(v)
	}
	r, _ = ParseCGMiner([]byte(`{"STATS":[{}, {"temp1":61,"temp3":63}]}`))
	v = originalValues(r)
	if len(v) != 1 {
		t.Fatal(v)
	}
}

func TestAvalonMTavgSurvivesGenericZeroInEitherOrder(t *testing.T) {
	for _, key := range []string{"MM ID0", avalonFlatKey} {
		data, _ := json.Marshal(map[string]any{"STATS": []any{map[string]any{key: flat1566}}})
		board, e := ParseCGMiner(data)
		if e != nil {
			t.Fatal(e)
		}
		zero := Reading{Brand: "Avalon", TempC: ptr(0)}
		setBTCTemperatures(&zero, []*float64{ptr(0)}, "Avalon LuCI temp")
		for _, merged := range []Reading{merge(board, zero), merge(zero, board)} {
			if got := originalValues(merged); !reflect.DeepEqual(got, []float64{66, 68, 63, 64}) {
				t.Fatal(key, got)
			}
			value(t, merged.TempC, 82)
			if len(merged.Boards) != 4 {
				t.Fatal(merged.Boards)
			}
		}
		if hasAvalonMMMeans(board) != (key == "MM ID0") {
			t.Fatal("incorrect HTTP fallback decision")
		}
	}
}
func TestActualReportedZeroIsNotInventedOrSilentlyReplaced(t *testing.T) {
	r := Reading{Brand: "Avalon"}
	setBTCTemperatures(&r, []*float64{ptr(0)}, "Avalon LuCI temp")
	if got := originalValues(merge(Reading{}, r)); len(got) != 1 || got[0] != 0 {
		t.Fatal(got)
	}
	empty := merge(Reading{}, Reading{})
	if empty.TempC != nil || len(originalValues(empty)) != 0 {
		t.Fatal("missing became zero")
	}
}
