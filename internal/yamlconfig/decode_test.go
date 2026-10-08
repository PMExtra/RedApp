package yamlconfig

import (
	"strings"
	"testing"
)

func TestStrictTypedValuesAndDiagnostics(t *testing.T) {
	type sample struct {
		S string   `json:"s"`
		B bool     `json:"b"`
		N int      `json:"n"`
		L []string `json:"l"`
	}
	var got sample
	if e := Decode([]byte("s: ''\nb: false\nn: 0\nl: []\n"), "sample.yaml", 1<<20, &got); e != nil || got.S != "" || got.B || got.N != 0 || got.L == nil {
		t.Fatalf("explicit empty values changed: %+v %v", got, e)
	}
	for _, raw := range []string{"", "s: 1", "s: &value x", "s: *value", "s: !custom x", "s: null", "s: a\ns: b", "s: x\n---\ns: y", "unknown: x", "s: x\n<<: {s: y}", "s: 1.5"} {
		if e := Decode([]byte(raw), "sample.yaml", 1<<20, &got); e == nil || !strings.Contains(e.Error(), "sample.yaml") {
			t.Errorf("accepted %q or missing filename: %v", raw, e)
		}
	}
	if e := Decode([]byte("s: x\nunknown: y"), "sample.yaml", 1<<20, &got); e == nil || !strings.Contains(e.Error(), ":2:") || !strings.Contains(e.Error(), "unknown") {
		t.Fatal(e)
	}
	raw := []byte(strings.Repeat("a: {", 34) + "b: x" + strings.Repeat("}", 34))
	if _, e := JSON(raw, 1<<20, "deep.yaml"); e == nil {
		t.Fatal("accepted depth overflow")
	}
}
