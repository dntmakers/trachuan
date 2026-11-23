package classify

import "testing"

func TestClassificationEligible(t *testing.T) {
	for _, tc := range []struct {
		name, class, kind, text string
		covered, want           bool
	}{
		{"article", "primary", "dieu", "Substantive legal text", false, true},
		{"other method", "primary", "dieu", "Substantive legal text", true, false},
		{"fragment", "primary", "dieu", "Luật này có hiệu lực từ ngày 01/01/2026.", false, false},
		{"clause", "primary", "khoan", "Substantive legal text", false, false},
		{"secondary", "secondary", "dieu", "Substantive legal text", false, false},
		{"context", "relation_context", "dieu", "Substantive legal text", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classificationEligible(tc.class, tc.kind, tc.text, tc.covered); got != tc.want {
				t.Fatalf("eligible=%v want %v", got, tc.want)
			}
		})
	}
}
