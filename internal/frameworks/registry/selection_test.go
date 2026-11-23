package registry

import (
	"strings"
	"testing"
)

func TestParseSourceSelection(t *testing.T) {
	base := strings.NewReplacer("role: text", "role: catalog", "format: txt", "format: xlsx", "parser: \"\"", "parser: csa-ccm-4-1-xlsx").Replace(validRegistry())
	for _, tc := range []struct {
		name, source string
		ok           bool
	}{
		{"matching", "alpha.txt", true},
		{"missing", "", false},
		{"unknown", "other.xlsx", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := strings.Replace(base, "    expected_controls:", "    parse_source: \""+tc.source+"\"\n    expected_controls:", 1)
			_, err := Parse(strings.NewReader(input))
			if (err == nil) != tc.ok {
				t.Fatalf("Parse success = %v, want %v", err == nil, tc.ok)
			}
		})
	}
}
