package cli

import "testing"

func TestParseClassifyPack(t *testing.T) {
	args := []string{"vn", "20261121_1", "--runs", "runs", "--parts", "32", "--baseline", "baseline"}
	o, err := parseClassifyPack(args)
	if err != nil || o.country != "vn" || o.release != "20261121_1" || o.runs != "runs" || o.baseline != "baseline" || o.parts != 32 {
		t.Fatalf("invalid pack options: %+v, %v", o, err)
	}
	for _, bad := range [][]string{args[:6], append(append([]string{}, args...), "--parts", "1"), {"vn", "20261121_1", "--runs", "runs", "--parts", "65", "--baseline", "baseline"}} {
		if _, err := parseClassifyPack(bad); err == nil {
			t.Fatalf("invalid pack arguments accepted: %v", bad)
		}
	}
}
