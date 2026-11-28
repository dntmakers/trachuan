package parse

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func pciNormalizeFixture(pages []pdftext.Page) {
	for pi := range pages {
		for bi := range pages[pi].Blocks {
			for li := range pages[pi].Blocks[bi].Lines {
				line := &pages[pi].Blocks[bi].Lines[li]
				if line.YMin != "" || len(line.Words) == 0 {
					continue
				}
				top := math.Inf(1)
				for _, w := range line.Words {
					y, err := strconv.ParseFloat(w.YMin, 64)
					if err == nil && !math.IsNaN(y) && y < top {
						top = y
					}
				}
				if math.IsInf(top, 1) {
					top = 0
				}
				line.YMin = strconv.FormatFloat(top, 'f', -1, 64)
			}
		}
	}
}

func pciHeaderForPage(page pdftext.Page, previous map[[2]float64]pciHeader) (pciHeader, bool, error) {
	pages := []pdftext.Page{page}
	pciNormalizeFixture(pages)
	page = pages[0]
	lines, err := pciClassifyPage(page)
	if err != nil {
		return pciHeader{}, false, err
	}
	return pciHeaderForLines(lines, previous)
}

func pciWord(x, y, text string) pdftext.Word {
	return pdftext.Word{XMin: x, YMin: y, XMax: x, YMax: y, Text: text}
}
func pciBlock(words ...pdftext.Word) pdftext.Block {
	return pdftext.Block{Lines: []pdftext.Line{{Words: words}}}
}
func parsePCIFixture(_ []string, pages []pdftext.Page, sha string, expected *int) (*Result, error) {
	pciNormalizeFixture(pages)
	return parsePCIProfile(pages, sha, expected, false)
}

func pciWantNodes(body string) []Node {
	return []Node{{Citation: "1", CitationNorm: "1", Kind: "requirement", Parent: -1, Ordinal: 0, PublisherTitle: "Invented", Status: "active"}, {Citation: "1.1", CitationNorm: "1.1", Kind: "requirement", Parent: 0, Ordinal: 1, PublisherTitle: "Invented", Status: "active"}, {Citation: "1.1.1", CitationNorm: "1.1.1", Kind: "requirement", Parent: 1, Ordinal: 2, Body: body, Status: "active"}}
}

func pciOuter() pdftext.Block {
	return pciBlock(pciWord("190", "90", "Requirements"), pciWord("210", "90", "invented"), pciWord("220", "90", "invented"), pciWord("240", "90", "Procedures"))
}
func pciFooter() pdftext.Block {
	line := pdftext.Line{Words: []pdftext.Word{pciWord("72", "540", "Payment")}}
	copyright := pdftext.Line{Words: []pdftext.Word{pciWord("72", "550", "©")}}
	for i := 0; i < 9; i++ {
		line.Words = append(line.Words, pciWord("80", "540", "invented"))
		copyright.Words = append(copyright.Words, pciWord("80", "550", "invented"))
	}
	copyright.Words = append(copyright.Words, pciWord("80", "550", "invented"))
	line.Words = append(line.Words, pciWord("100", "540", "v4.0.1"))
	copyright.Words = append(copyright.Words, pciWord("100", "550", "Reserved."))
	return pdftext.Block{Lines: []pdftext.Line{line, copyright, {Words: []pdftext.Word{pciWord("610", "540", "June"), pciWord("650", "540", "2024")}}, {Words: []pdftext.Word{pciWord("610", "550", "Page"), pciWord("650", "550", "1")}}}}
}
func pciSub(x, y string, left bool) pdftext.Block {
	words := []pdftext.Word{pciWord(x, y, "Defined"), pciWord("110", y, "invented")}
	if !left {
		words[1] = pciWord("310", y, "invented")
		words = append(words, pciWord("320", y, "invented"), pciWord("330", y, "Procedures"))
	} else {
		words = append(words, pciWord("120", y, "Requirements"))
	}
	return pciBlock(words...)
}

func pciFixture() ([]string, []pdftext.Page) {
	layout := make([]string, 372)
	pages := make([]pdftext.Page, 372)
	pages[41].Blocks = []pdftext.Block{
		pciBlock(pciWord("72", "75", "Requirement"), pciWord("130", "75", "1:"), pciWord("145", "75", "Invented")),
		pciOuter(),
		pciBlock(pciWord("590", "90", "Guidance")),
		pciBlock(pciWord("78", "125", "1.1"), pciWord("100", "125", "Invented")),
		pciSub("78", "145", true), pciSub("298", "145", false),
		pciBlock(pciWord("78", "165", "1.1.1"), pciWord("115", "165", "Invented")),
		pciFooter(),
	}
	pciNormalizeFixture(pages)
	return layout, pages
}
func TestPCI_Step1(t *testing.T) {
	layout, pages := pciFixture()
	count := 3
	got, err := parsePCIFixture(layout, pages, pciSHA, &count)
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{{Citation: "1", CitationNorm: "1", Kind: "requirement", Parent: -1, Ordinal: 0, PublisherTitle: "Invented", Status: "active"}, {Citation: "1.1", CitationNorm: "1.1", Kind: "requirement", Parent: 0, Ordinal: 1, PublisherTitle: "Invented", Status: "active"}, {Citation: "1.1.1", CitationNorm: "1.1.1", Kind: "requirement", Parent: 1, Ordinal: 2, Body: "Invented", Status: "active"}}
	if !reflect.DeepEqual(got.Nodes, want) {
		t.Fatalf("got %#v want %#v", got.Nodes, want)
	}
}
func TestPCI_RejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*[]string, *[]pdftext.Page, *string, *int)
	}{
		{"fingerprint", func(_ *[]string, _ *[]pdftext.Page, sha *string, _ *int) { *sha = "wrong" }},
		{"missing parent", func(_ *[]string, p *[]pdftext.Page, _ *string, _ *int) {
			(*p)[41].Blocks[6].Lines[0].Words[0].Text = "1.2.1"
		}},
		{"duplicate", func(_ *[]string, p *[]pdftext.Page, _ *string, _ *int) {
			(*p)[41].Blocks = append((*p)[41].Blocks, (*p)[41].Blocks[6])
		}},
		{"missing region", func(_ *[]string, p *[]pdftext.Page, _ *string, _ *int) { *p = (*p)[:41] }},
		{"malformed id", func(_ *[]string, p *[]pdftext.Page, _ *string, _ *int) {
			(*p)[41].Blocks = append((*p)[41].Blocks, pciBlock(pciWord("78", "180", "1.1.01")))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, p := pciFixture()
			sha := pciSHA
			n := 3
			tc.change(&l, &p, &sha, &n)
			want := map[string]string{"fingerprint": "source fingerprint mismatch", "missing parent": "missing requirement parent", "duplicate": "ambiguous line ownership", "missing region": "missing required region", "malformed id": "invalid requirement"}[tc.name]
			if _, err := parsePCIFixture(l, p, sha, &n); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %s, got %v", want, err)
			}
		})
	}
}
