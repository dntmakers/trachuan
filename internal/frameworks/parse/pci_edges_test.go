package parse

import (
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestPCI_Guards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]string, []pdftext.Page)
	}{
		{"missing footer", func(_ []string, p []pdftext.Page) { p[41].Blocks = p[41].Blocks[:7] }},
		{"unowned later line", func(_ []string, p []pdftext.Page) {
			b := pciBlock(pciWord("310", "120", "invented"))
			b.Lines = append(b.Lines, pdftext.Line{Words: []pdftext.Word{pciWord("80", "121", "invented")}})
			p[41].Blocks = append(p[41].Blocks, b)
		}},
		{"unpaired right header", func(_ []string, p []pdftext.Page) { p[41].Blocks = append(p[41].Blocks[:4], p[41].Blocks[5:]...) }},
		{"crossing testing cut", func(_ []string, p []pdftext.Page) {
			p[41].Blocks[6].Lines[0].Words = append(p[41].Blocks[6].Lines[0].Words, pdftext.Word{XMin: "294", XMax: "296", YMin: "165", YMax: "166", Text: "invented"})
		}},
		{"crossing guidance cut", func(_ []string, p []pdftext.Page) {
			p[41].Blocks[6].Lines[0].Words = append(p[41].Blocks[6].Lines[0].Words, pdftext.Word{XMin: "355", XMax: "357", YMin: "165", YMax: "166", Text: "invented"})
		}},
		{"input limit", func(_ []string, p []pdftext.Page) {
			p[0].Blocks = []pdftext.Block{pciBlock(pciWord("1", "1", strings.Repeat("x", 50_000_001)))}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, p := pciFixture()
			tc.change(l, p)
			n := 3
			want := map[string]string{"missing footer": "invalid table footer", "unowned later line": "unowned table text", "unpaired right header": "invalid paired header", "crossing testing cut": "crossing table band", "crossing guidance cut": "crossing table band", "input limit": "input limit exceeded"}[tc.name]
			if _, err := parsePCIFixture(l, p, pciSHA, &n); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %s, got %v", want, err)
			}
		})
	}
}

func TestPCI_Continuation(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		left     bool
		fail     bool
	}{
		{"active", "1.1.1", true, false},
		{"unknown", "1.1.2", false, true},
		{"group caption", "1.1", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, p := pciFixture()
			caption := pciBlock(pciWord("78", "180", tc.id), pciWord("110", "180", "(continued)"))
			if tc.left {
				caption.Lines[0].Words = append(caption.Lines[0].Words, pciWord("180", "180", "invented"))
			}
			p[41].Blocks = append(p[41].Blocks, caption)
			n := 3
			_, err := parsePCIFixture(l, p, pciSHA, &n)
			if (err != nil) != tc.fail {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
