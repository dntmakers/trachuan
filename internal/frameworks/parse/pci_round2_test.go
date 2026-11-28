package parse

import (
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestPCI_ReviewRound2(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		change                       func([]pdftext.Page)
		count                        int
		wantErr, wantTitle, wantBody string
	}{
		{"subheader above outer bottom", func(p []pdftext.Page) {
			p[41].Blocks[1].Lines[0].Words[3].YMax = "101"
			p[41].Blocks[4] = pciSub("78", "100", true)
			p[41].Blocks[5] = pciSub("298", "100", false)
		}, 3, "paired header", "", ""},
		{"subheader crosses cut", func(p []pdftext.Page) { w := &p[41].Blocks[4].Lines[0].Words[2]; w.XMin = "294"; w.XMax = "296" }, 3, "structural", "", ""},
		{"subheader above outer", func(p []pdftext.Page) {
			p[41].Blocks[4] = pciSub("78", "80", true)
			p[41].Blocks[5] = pciSub("298", "80", false)
		}, 3, "paired header", "", ""},
		{"testing marker hides opener", func(p []pdftext.Page) {
			p[41].Blocks[6].Lines = append(p[41].Blocks[6].Lines, pdftext.Line{Words: []pdftext.Word{pciWord("78", "190", "invented")}})
			p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "180", "1.1.2"), pciWord("310", "180", "(continued)"), pciWord("120", "180", "invented")))
		}, 4, "ownership", "", ""},
		{"opener baseline collision", func(p []pdftext.Page) {
			p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "165", "1.1.2"), pciWord("120", "165", "invented")))
		}, 4, "ownership", "", ""},
		{"caption in group title", func(p []pdftext.Page) {
			p[41].Blocks[3].Lines = append(p[41].Blocks[3].Lines, pdftext.Line{Words: []pdftext.Word{pciWord("78", "135", "(continued)")}})
		}, 3, "", "Invented (continued)", ""},
		{"subheader in group title", func(p []pdftext.Page) {
			p[41].Blocks[3].Lines = append(p[41].Blocks[3].Lines, pciSub("78", "130", true).Lines[0])
			p[41].Blocks = append(p[41].Blocks, pciSub("298", "130", false))
		}, 3, "", "Invented", ""},
		{"later continuation word", func(p []pdftext.Page) {
			p[41].Blocks = append(p[41].Blocks, pdftext.Block{Lines: []pdftext.Line{{Words: []pdftext.Word{pciWord("78", "180", "1.1.1"), pciWord("110", "180", "(continued)"), pciWord("160", "180", "first")}}, {Words: []pdftext.Word{pciWord("78", "190", "(continued)"), pciWord("150", "190", "invented"), pciWord("190", "190", "end")}}}})
		}, 3, "", "", "Invented\n\nfirst (continued) invented end"},
		{"whitespace after marker", func(p []pdftext.Page) {
			p[95].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciBlock(pciWord("78", "120", "(continued)")), pciBlock(pciWord("78", "130", "   ")), pciFooter()}
		}, 3, "missing standalone continuation", "", ""},
		{"marker in excluded band", func(p []pdftext.Page) {
			p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("310", "180", "(continued)")))
		}, 3, "", "", "Invented"},
		{"marker crossing left band", func(p []pdftext.Page) {
			w := pciWord("72", "120", "(continued)")
			w.XMax = "74"
			p[95].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciBlock(w), pciBlock(pciWord("78", "130", "invented")), pciFooter()}
		}, 3, "standalone", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, p := pciFixture()
			tc.change(p)
			n := tc.count
			r, err := parsePCIFixture(l, p, pciSHA, &n)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected %s, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantTitle != "" && r.Nodes[1].PublisherTitle != tc.wantTitle {
				t.Fatalf("title %q", r.Nodes[1].PublisherTitle)
			}
			if tc.wantBody != "" && r.Nodes[2].Body != tc.wantBody {
				t.Fatalf("body %q", r.Nodes[2].Body)
			}
		})
	}
}
