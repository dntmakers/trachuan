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

func TestPDFSourceSelection(t *testing.T) {
	for _, tc := range []struct {
		name, source, role, format string
		ok                         bool
	}{
		{"matching", "chosen.pdf", "text", "pdf", true},
		{"wrong role", "chosen.pdf", "catalog", "pdf", false},
		{"wrong format", "chosen.pdf", "text", "xlsx", false},
		{"other version", "other.pdf", "text", "pdf", false},
		{"missing path", "absent.pdf", "text", "pdf", false},
		{"no selector", "", "text", "pdf", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := Framework{Parser: "iso27002-2022-pdf", ParseSource: tc.source, Files: []File{{Path: "chosen.pdf", Role: tc.role, Format: tc.format}}}
			_, err := v.SelectedSource()
			if (err == nil) != tc.ok {
				t.Fatalf("SelectedSource error = %v, want success %v", err, tc.ok)
			}
		})
	}
}

func TestPDFSourceSelectionRejectsDuplicatePaths(t *testing.T) {
	f := Framework{Parser: "iso27002-2022-pdf", ParseSource: "same.pdf", Files: []File{{Path: "same.pdf", Role: "text", Format: "pdf"}, {Path: "same.pdf", Role: "text", Format: "pdf"}}}
	if _, err := f.SelectedSource(); err == nil {
		t.Fatal("duplicate path accepted")
	}
}
