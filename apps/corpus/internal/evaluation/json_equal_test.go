package evaluation

import (
	"encoding/json"
	"testing"
)

func TestJSONEqual_ComparesNumbersExactly(t *testing.T) {
	for _, tc := range []struct {
		a, b     string
		sameKind bool
		want     bool
	}{
		{"9007199254740993", "9007199254740992", false, false},
		{"9007199254740993", "9007199254740993", true, true},
		{"1", "1.0", false, true},
		{"1", "1.0", true, false},
		{"1", "1e0", false, true},
		{"0.50", "0.5", true, true},
		{"9007199254740993", "9007199254740992.0", false, false},
		{"9007199254740992", "9007199254740992.0", false, true},
		{"1", "1.5", false, false},
		{"-0", "0", false, true},
	} {
		got := jsonEqual(json.Number(tc.a), json.Number(tc.b), tc.sameKind)
		if got != tc.want {
			t.Errorf("jsonEqual(%s, %s, %v) = %v, want %v", tc.a, tc.b, tc.sameKind, got, tc.want)
		}
	}
}
