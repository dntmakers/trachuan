package parse

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestPCI_CountsFlattenedWordsOnce(t *testing.T) {
	layout, pages := pciFixture()
	w := pciWord("1", "1", strings.Repeat("x", 26_000_000))
	pages[0] = pdftext.Page{Words: []pdftext.Word{w}, Blocks: []pdftext.Block{pciBlock(w)}}
	n := 3
	if _, err := parsePCIFixture(layout, pages, pciSHA, &n); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPCI_RequiresEditionInventory(t *testing.T) {
	_, pages := pciFixture()
	n := 3
	_, err := parsePCIDSS(pages, pciSHA, &n)
	if err == nil || !strings.Contains(err.Error(), "missing required region") {
		t.Fatalf("expected missing required region, got %v", err)
	}
}

func TestPCI_RejectsLinePastNextOpener(t *testing.T) {
	l, p := pciFixture()
	p[41].Blocks[6].Lines = append(p[41].Blocks[6].Lines, pdftext.Line{Words: []pdftext.Word{pciWord("78", "190", "invented")}})
	p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "180", "1.1.2"), pciWord("120", "180", "invented")))
	n := 4
	_, err := parsePCIFixture(l, p, pciSHA, &n)
	if err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("expected ownership error, got %v", err)
	}
}

func TestPCI_RejectsUnexpectedStandalonePage(t *testing.T) {
	l, p := pciFixture()
	p[42].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciBlock(pciWord("78", "120", "(continued)")), pciBlock(pciWord("78", "130", "invented")), pciFooter()}
	n := 3
	_, err := parsePCIFixture(l, p, pciSHA, &n)
	if err == nil || !strings.Contains(err.Error(), "standalone") {
		t.Fatalf("expected standalone error, got %v", err)
	}
}

func TestPCI_RejectsCaptionMarkerInTestingBand(t *testing.T) {
	l, p := pciFixture()
	p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "180", "1.1.1"), pciWord("310", "180", "(continued)")))
	n := 3
	_, err := parsePCIFixture(l, p, pciSHA, &n)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestPCI_RejectsCaptionCrossingCut(t *testing.T) {
	l, p := pciFixture()
	b := pciBlock(pciWord("78", "130", "1.1"), pciWord("110", "130", "(continued)"), pdftext.Word{XMin: "294", XMax: "296", YMin: "130", YMax: "131", Text: "invented"})
	p[41].Blocks = append(p[41].Blocks, b)
	n := 3
	_, err := parsePCIFixture(l, p, pciSHA, &n)
	if err == nil || !strings.Contains(err.Error(), "crossing") {
		t.Fatalf("expected crossing error, got %v", err)
	}
}

func TestPCI_HeaderLinesDoNotHideBody(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func([]pdftext.Page)
	}{
		{"body after subheader", func(p []pdftext.Page) {
			p[41].Blocks[4].Lines = append(p[41].Blocks[4].Lines, pdftext.Line{Words: []pdftext.Word{pciWord("78", "170", "invented")}})
		}},
		{"subheader after body", func(p []pdftext.Page) {
			p[41].Blocks[6].Lines = append(p[41].Blocks[6].Lines, pciSub("78", "180", true).Lines[0])
			p[41].Blocks = append(p[41].Blocks, pciSub("298", "180", false))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, p := pciFixture()
			tc.mutate(p)
			n := 3
			r, err := parsePCIFixture(l, p, pciSHA, &n)
			if err != nil {
				t.Fatal(err)
			}
			body := r.Nodes[2].Body
			if tc.name == "body after subheader" && !strings.Contains(body, "invented") {
				t.Fatalf("missing body: %q", body)
			}
			if tc.name == "subheader after body" && strings.Contains(body, "Defined") {
				t.Fatalf("header in body: %q", body)
			}
		})
	}
}

func TestPCI_ReviewGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]pdftext.Page, *int)
		want   string
	}{
		{"total mismatch", func(_ []pdftext.Page, n *int) { *n = 4 }, "control count mismatch"},
		{"wrong active root", func(p []pdftext.Page, _ *int) { p[41].Blocks[6].Lines[0].Words[0].Text = "2.1.1" }, "wrong requirement root"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, p := pciFixture()
			n := 3
			tc.change(p, &n)
			_, err := parsePCIFixture(l, p, pciSHA, &n)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s, got %v", tc.want, err)
			}
		})
	}
}
func TestPCI_LaterLineIDIsText(t *testing.T) {
	l, p := pciFixture()
	p[41].Blocks[6].Lines = append(p[41].Blocks[6].Lines, pdftext.Line{Words: []pdftext.Word{pciWord("78", "175", "1.1.2"), pciWord("120", "175", "invented")}})
	n := 3
	r, err := parsePCIFixture(l, p, pciSHA, &n)
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{{Citation: "1", CitationNorm: "1", Kind: "requirement", Parent: -1, Ordinal: 0, PublisherTitle: "Invented", Status: "active"}, {Citation: "1.1", CitationNorm: "1.1", Kind: "requirement", Parent: 0, Ordinal: 1, PublisherTitle: "Invented", Status: "active"}, {Citation: "1.1.1", CitationNorm: "1.1.1", Kind: "requirement", Parent: 1, Ordinal: 2, Body: "Invented 1.1.2 invented", Status: "active"}}
	if !reflect.DeepEqual(r.Nodes, want) {
		t.Fatalf("nodes differ: %#v", r.Nodes)
	}
}

func TestPCI_FooterRejectsSingleWordCandidate(t *testing.T) {
	_, p := pciFixture()
	p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "531", "Payment"), pciWord("120", "531", "invented")))
	h, _, err := pciHeaderForPage(p[41], nil)
	if err != nil || h.footer != 540 {
		t.Fatalf("expected footer at 540, got %v and error %v", h.footer, err)
	}
}

func TestPCI_RejectsMalformedStructuralLabel(t *testing.T) {
	_, p := pciFixture()
	p[41].Blocks[4].Lines[0].Words = append(p[41].Blocks[4].Lines[0].Words[:1], p[41].Blocks[4].Lines[0].Words[2])
	_, _, err := pciHeaderForPage(p[41], nil)
	if err == nil || !strings.Contains(err.Error(), "structural") {
		t.Fatalf("expected structural error, got %v", err)
	}
}

func TestPCI_RejectsFooterNotLast(t *testing.T) {
	_, p := pciFixture()
	p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("72", "560", "invented")))
	_, _, err := pciHeaderForPage(p[41], nil)
	if err == nil || !strings.Contains(err.Error(), "footer") {
		t.Fatalf("expected footer error, got %v", err)
	}
}

func TestPCI_ProcessesBlockWithEmptyFirstLine(t *testing.T) {
	l, p := pciFixture()
	p[41].Blocks = append(p[41].Blocks, pdftext.Block{Lines: []pdftext.Line{{}, {Words: []pdftext.Word{pciWord("78", "180", "invented")}}}})
	n := 3
	r, err := parsePCIFixture(l, p, pciSHA, &n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Nodes[2].Body, "invented") {
		t.Fatalf("missing body: %q", r.Nodes[2].Body)
	}
}

func TestPCI_RejectsFooterInterloper(t *testing.T) {
	_, p := pciFixture()
	p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("72", "545", "invented")))
	_, _, err := pciHeaderForPage(p[41], nil)
	if err == nil || !strings.Contains(err.Error(), "footer") {
		t.Fatalf("expected footer error, got %v", err)
	}
}

func TestPCI_RejectsMissingStandaloneCaption(t *testing.T) {
	if err := pciValidateStandalone(map[int]bool{96: true}); err == nil || !strings.Contains(err.Error(), "standalone") {
		t.Fatalf("expected standalone error, got %v", err)
	}
}

func TestPCI_RejectsCrossingAfterEmptyFirstLine(t *testing.T) {
	l, p := pciFixture()
	p[41].Blocks = append(p[41].Blocks, pdftext.Block{Lines: []pdftext.Line{{}, {Words: []pdftext.Word{pciWord("78", "170", "invented")}}, {Words: []pdftext.Word{pciWord("78", "190", "invented")}}}}, pciBlock(pciWord("78", "180", "1.1.2"), pciWord("120", "180", "invented")))
	n := 4
	_, err := parsePCIFixture(l, p, pciSHA, &n)
	if err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("expected ownership error, got %v", err)
	}
}

func TestPCI_RejectsRootWithoutTable(t *testing.T) {
	roots := make([]Node, 0, 16)
	tables := map[string]bool{}
	for i := 1; i <= 12; i++ {
		id := strconv.Itoa(i)
		roots = append(roots, Node{Citation: id, Parent: -1})
		tables[id] = true
	}
	for _, id := range []string{"A1", "A2", "A3"} {
		roots = append(roots, Node{Citation: id, Parent: -1})
		tables[id] = true
	}
	roots = append(roots, Node{Citation: "A3.1", Parent: 14})
	delete(tables, "5")
	if err := pciValidateInventory(roots, tables); err == nil || !strings.Contains(err.Error(), "missing required region") {
		t.Fatalf("expected missing region, got %v", err)
	}
}

func TestPCIBody_RejectsAmbiguousGlyph(t *testing.T) {
	body := pciBody{lines: []pciBodyLine{{page: 1, block: 1, key: pciKey{x: 90, y: 99}, geometry: pciBox{xmin: 90, ymin: 99}, text: "one"}, {page: 1, block: 2, key: pciKey{x: 78, y: 100}, geometry: pciBox{xmin: 78, ymin: 100}, text: "•"}, {page: 1, block: 3, key: pciKey{x: 90, y: 101}, geometry: pciBox{xmin: 90, ymin: 101}, text: "two"}}}
	_, err := body.text()
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous glyph error, got %v", err)
	}
}
