package parse

import (
	"reflect"
	"strings"
	"testing"
)

func TestAICPA_RejectsWrongFingerprint(t *testing.T) {
	count := 1
	_, err := ParsePages("aicpa-tsc-2017-pdf", []string{"invented"}, "", "", "wrong", &count)
	if err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatal("expected fingerprint rejection")
	}
}

func TestAICPA_Criteria(t *testing.T) {
	pages := aicpaFixture()
	pages[13] += "CC1.1    First statement\n         wraps here.\n         The following points of focus:\n                      • Invented title [tag] — Invented detail\nPage 14\n"
	pages[14] += "                          continued detail\n                              • Nested detail\n         Additional points of focus apply to\n         the following groups:\n                      • Second detail\nA1.1     Another statement\n         across a page\nPage 15\n"
	pages[15] += "         ends here.\nP1.0     CATEGORY\n         CATEGORY LABEL\nP1.1     Last statement.\n                      • Untitled detail\nPage 16\n"
	pages = append(pages, "                      • Outside table")
	count := 6
	got, err := ParsePages("aicpa-tsc-2017-pdf", pages, "", "", "23a099dd1dc273b7a45fda2351f376344c33fcb2911d586036c0e0d631b8ad69", &count)
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{
		{Citation: "CC1.1", CitationNorm: "CC1.1", Kind: "criterion", Parent: -1, Ordinal: 0, Body: "First statement wraps here.", Status: "active"},
		{Citation: "CC1.1-pof-01", CitationNorm: "CC1.1-POF-01", Kind: "point-of-focus", Parent: 0, Ordinal: 1, PublisherTitle: "Invented title", Body: "Invented title [tag] — Invented detail continued detail\n• Nested detail", Status: "active"},
		{Citation: "CC1.1-pof-02", CitationNorm: "CC1.1-POF-02", Kind: "point-of-focus", Parent: 0, Ordinal: 2, Body: "Second detail", Status: "active"},
		{Citation: "A1.1", CitationNorm: "A1.1", Kind: "criterion", Parent: -1, Ordinal: 3, Body: "Another statement across a page ends here.", Status: "active"},
		{Citation: "P1.1", CitationNorm: "P1.1", Kind: "criterion", Parent: -1, Ordinal: 4, Body: "Last statement.", Status: "active"},
		{Citation: "P1.1-pof-01", CitationNorm: "P1.1-POF-01", Kind: "point-of-focus", Parent: 4, Ordinal: 5, Body: "Untitled detail", Status: "active"},
	}
	if !reflect.DeepEqual(got.Nodes, want) {
		t.Fatalf("nodes differ: %#v", got.Nodes)
	}
}

func aicpaFixture() []string {
	pages := make([]string, 67)
	for i := 13; i < 67; i++ {
		pages[i] = "ABC      " + strings.Repeat("X", 43) + "\nAbc. #\n"
	}
	pages[13] = strings.Repeat("\n", 24) + pages[13]
	return pages
}

func TestAICPA_SafeErrors(t *testing.T) {
	const sentinel = "invented-private-sentinel"
	tests := []struct {
		name   string
		want   string
		mutate func([]string) []string
	}{
		{"missing region", "missing table region", func(p []string) []string { return p[:20] }},
		{"missing header", "missing table header", func(p []string) []string { p[14] = sentinel; return p }},
		{"shifted second header", "invalid table header", func(p []string) []string { p[14] = strings.Replace(p[14], "Abc. #", " Abc. #", 1); return p }},
		{"duplicate criterion", "duplicate criterion", func(p []string) []string { p[13] += "CC1.1    Invented statement\nCC1.1    " + sentinel; return p }},
		{"inconsistent bullet columns", "inconsistent bullet column", func(p []string) []string {
			p[13] += "CC1.1    Invented statement\n                      • First detail\n                     • " + sentinel
			return p
		}},
		{"unfinished introduction", "incomplete focus introduction", func(p []string) []string {
			p[13] += "CC1.1    Invented statement\n         Following points of focus\nCC1.2    " + sentinel
			return p
		}},
		{"oversized pages", "page text limit exceeded", func(p []string) []string { p[0] = strings.Repeat("x", 50_000_001); return p }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := 1
			_, err := ParsePages("aicpa-tsc-2017-pdf", tt.mutate(aicpaFixture()), "", "", aicpaSHA, &n)
			if err == nil || err.Error() != tt.want || strings.Contains(err.Error(), sentinel) {
				t.Fatal("missing safe error")
			}
		})
	}
}

func TestAICPA_ReviewRegressions(t *testing.T) {
	t.Run("introduction and sublabel", func(t *testing.T) {
		p := aicpaFixture()
		p[13] += "CC1.1    Invented statement\n                      • First detail\n         Additional points of focus:\n         Invented sublabel:\n                      • Second detail\n"
		n := 3
		got, err := ParsePages("aicpa-tsc-2017-pdf", p, "", "", aicpaSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		if got.Nodes[1].Body != "First detail" || got.Nodes[2].Body != "Second detail" {
			t.Fatal("incorrect bullet boundaries")
		}
	})
	t.Run("page edges", func(t *testing.T) {
		p := aicpaFixture()
		p[13] += "CC1.1    Invented statement\n                      • First detail\nPage 14\n"
		p[14] += "\n\n                          continued\n                          Page 77\n                          after page label\nPage 15\n"
		n := 2
		got, err := ParsePages("aicpa-tsc-2017-pdf", p, "", "", aicpaSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		if got.Nodes[1].Body != "First detail continued Page 77 after page label" {
			t.Fatal("incorrect page continuation")
		}
	})
	t.Run("shifted statement", func(t *testing.T) {
		p := aicpaFixture()
		p[13] += "CC1.1     Invented statement\n"
		n := 1
		_, err := ParsePages("aicpa-tsc-2017-pdf", p, "", "", aicpaSHA, &n)
		if err == nil || err.Error() != "invalid criterion row" {
			t.Fatal("expected invalid criterion row")
		}
	})
	t.Run("tab padding", func(t *testing.T) {
		p := aicpaFixture()
		p[13] += "CC1.1\t   Invented statement\n"
		n := 1
		_, err := ParsePages("aicpa-tsc-2017-pdf", p, "", "", aicpaSHA, &n)
		if err == nil || err.Error() != "invalid criterion row" {
			t.Fatal("expected invalid criterion row")
		}
	})
	t.Run("missing statement", func(t *testing.T) {
		p := aicpaFixture()
		p[13] += "CC1.1\n"
		n := 1
		_, err := ParsePages("aicpa-tsc-2017-pdf", p, "", "", aicpaSHA, &n)
		if err == nil || err.Error() != "invalid criterion row" {
			t.Fatal("expected invalid criterion row")
		}
	})
	t.Run("orphan bullet", func(t *testing.T) {
		p := aicpaFixture()
		p[13] += "                      • Invented orphan\nCC1.1    Invented statement\n"
		n := 1
		_, err := ParsePages("aicpa-tsc-2017-pdf", p, "", "", aicpaSHA, &n)
		if err == nil || err.Error() != "orphan point of focus" {
			t.Fatal("expected orphan point of focus")
		}
	})
	t.Run("count mismatch", func(t *testing.T) {
		p := aicpaFixture()
		p[13] += "CC1.1    Invented statement\n"
		n := 2
		_, err := ParsePages("aicpa-tsc-2017-pdf", p, "", "", aicpaSHA, &n)
		if err == nil || err.Error() != "control count mismatch" {
			t.Fatal("expected count mismatch")
		}
	})
}
