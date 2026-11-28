package parse

import (
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestPCI_ReviewRound3(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		change                       func([]pdftext.Page)
		count                        int
		wantErr, wantTitle, wantBody string
	}{
		{"group with excluded caption marker", func(p []pdftext.Page) {
			p[41].Blocks[3] = pciBlock(pciWord("78", "125", "1.1"), pciWord("310", "125", "(continued)"), pciWord("130", "125", "Invented"))
		}, 3, "", "Invented (continued)", ""},
		{"root like later line", func(p []pdftext.Page) {
			p[42].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pdftext.Block{Lines: []pdftext.Line{{Words: []pdftext.Word{pciWord("74", "100", "first")}}, {Words: []pdftext.Word{pciWord("72", "105", "Requirement"), pciWord("120", "105", "2:"), pciWord("150", "105", "invented")}}}}, pciSub("59.8", "150", true), pciSub("284.8", "150", false), pciFooter()}
		}, 3, "", "", "Invented first Requirement 2: invented"},
		{"id-only opener crosses next row", func(p []pdftext.Page) {
			p[41].Blocks[6].Lines[0].Words = p[41].Blocks[6].Lines[0].Words[:1]
			p[41].Blocks[6].Lines = append(p[41].Blocks[6].Lines, pdftext.Line{Words: []pdftext.Word{pciWord("78", "190", "invented")}})
			p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "180", "1.1.2"), pciWord("120", "180", "invented")))
		}, 4, "ownership", "", ""},
		{"projection survives first word above header", func(p []pdftext.Page) {
			p[42].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciBlock(pciWord("310", "85", "excluded"), pciWord("78", "100", "invented")), pciFooter()}
		}, 3, "", "", "Invented invented"},
		{"crossing opener word", func(p []pdftext.Page) { p[41].Blocks[6].Lines[0].Words[0].XMax = "296" }, 3, "crossing", "", ""},
		{"body same baseline as opener", func(p []pdftext.Page) {
			p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("76", "180", "invented")), pciBlock(pciWord("78", "180", "1.1.2"), pciWord("120", "180", "invented")))
		}, 4, "ownership", "", ""},
		{"caption footer line", func(p []pdftext.Page) {
			p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "180", "1.1.2"), pciWord("120", "180", "invented")), pdftext.Block{Lines: []pdftext.Line{{Words: []pdftext.Word{pciWord("78", "200", "1.1.1"), pciWord("120", "200", "(continued)")}}, {Words: []pdftext.Word{pciWord("78", "540", "invented")}}}})
		}, 4, "", "", "Invented"},
		{"caption section label", func(p []pdftext.Page) {
			p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "180", "1.1.1"), pciWord("110", "180", "(continued)"), pciWord("150", "180", "Applicability"), pciWord("210", "180", "Notes"), pciWord("240", "180", "invented")))
		}, 3, "", "", "Invented\nApplicability Notes invented"},
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
			body := "Invented"
			if tc.wantBody != "" {
				body = tc.wantBody
			}
			want := pciWantNodes(body)
			if tc.wantTitle != "" {
				want[1].PublisherTitle = tc.wantTitle
			}
			if tc.count == 4 {
				want = append(want, Node{Citation: "1.1.2", CitationNorm: "1.1.2", Kind: "requirement", Parent: 1, Ordinal: 3, Body: "invented", Status: "active"})
			}
			if !reflect.DeepEqual(r.Nodes, want) {
				t.Fatalf("unexpected nodes: %#v", r.Nodes)
			}
		})
	}
}
