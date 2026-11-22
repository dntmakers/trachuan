package evaluation

import (
	"fmt"
	"strings"
	"testing"
)

func TestLoadWarningCases_ClosedFixture(t *testing.T) {
	valid := `{"entry_id":"effect-example","question":"Quy định gì?","target":"vn:vbpl:1#dieu-5","acting":"vn:vbpl:2#dieu-30/khoan-2","relation_kind":"repeals"}`
	var many []string
	for i := 0; i < 21; i++ {
		many = append(many, strings.Replace(valid, "effect-example", fmt.Sprintf("effect-%02d", i), 1))
	}
	for _, tc := range []struct {
		name  string
		data  string
		valid bool
	}{
		{"empty", `[]`, true},
		{"case", `[` + valid + `]`, true},
		{"null", `null`, false},
		{"duplicate key", `[` + strings.Replace(valid, `"entry_id":"effect-example"`, `"entry_id":"effect-example","entry_id":"effect-other"`, 1) + `]`, false},
		{"unknown field", `[` + strings.Replace(valid, `"entry_id":`, `"extra":1,"entry_id":`, 1) + `]`, false},
		{"missing field", `[` + strings.Replace(valid, `,"relation_kind":"repeals"`, ``, 1) + `]`, false},
		{"duplicate case", `[` + valid + `,` + valid + `]`, false},
		{"unsorted", `[` + strings.Replace(valid, "effect-example", "effect-z", 1) + `,` + valid + `]`, false},
		{"nul anchor", `[` + strings.Replace(valid, "dieu-5", `dieu-5\u0000`, 1) + `]`, false},
		{"nul key", `[` + strings.Replace(valid, "vn:vbpl:1", `vn:vbpl:1\u0000`, 1) + `]`, false},
		{"ambiguous anchor", `[` + strings.Replace(valid, "dieu-5", "dieu-5~2", 1) + `]`, false},
		{"same document", `[` + strings.Replace(valid, "vn:vbpl:2", "vn:vbpl:1", 1) + `]`, false},
		{"wrong country", `[` + strings.Replace(valid, "vn:vbpl:2", "us:vbpl:2", 1) + `]`, false},
		{"invalid relation", `[` + strings.Replace(valid, "repeals", "cites", 1) + `]`, false},
		{"empty question", `[` + strings.Replace(valid, "Quy định gì?", " ", 1) + `]`, false},
		{"too many cases", `[` + strings.Join(many, `,`) + `]`, false},
		{"too large", `[` + strings.Repeat(" ", 65536) + `]`, false},
		{"trailing data", `[] []`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadWarningCases([]byte(tc.data), "vn")
			if (err == nil) != tc.valid {
				t.Fatalf("LoadWarningCases() error = %v, want valid %v", err, tc.valid)
			}
		})
	}
}
