package parse

import (
	"reflect"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestPCI_Body(t *testing.T) {
	layout, pages := pciFixture()
	pages[41].Blocks[6] = pdftext.Block{Lines: []pdftext.Line{
		{Words: []pdftext.Word{pciWord("78", "165", "1.1.1"), pciWord("110", "165", "First"), pciWord("180", "165", "line"), pciWord("310", "165", "excluded")}},
		{Words: []pdftext.Word{pciWord("78", "175", "wrap")}},
	}}
	pages[41].Blocks = append(pages[41].Blocks,
		pciBlock(pciWord("78", "195", "Second"), pciWord("120", "195", "paragraph")),
		pciBlock(pciWord("78", "215", "•")), pciBlock(pciWord("92", "216", "Item"), pciWord("115", "216", "text")),
		pciBlock(pciWord("78", "235", "Customized"), pciWord("110", "235", "invented"), pciWord("130", "235", "Objective"), pciWord("150", "235", "purpose")),
		pciBlock(pciWord("78", "255", "Applicability"), pciWord("130", "255", "Notes"), pciWord("165", "255", "scope")),
	)
	pages[42].Blocks = []pdftext.Block{
		pciOuter(), pciBlock(pciWord("590", "90", "Guidance")),
		pciBlock(pciWord("78", "120", "1.1.1"), pciWord("120", "120", "(continued)"), pciWord("180", "120", "continues")),
		pciFooter(),
	}
	expected := 3
	result, err := parsePCIFixture(layout, pages, pciSHA, &expected)
	if err != nil {
		t.Fatal(err)
	}
	want := "First line wrap\n\nSecond paragraph\n• Item text\nCustomized invented Objective purpose\nApplicability Notes scope continues"
	if !reflect.DeepEqual(result.Nodes, pciWantNodes(want)) {
		t.Fatalf("unexpected nodes: %#v", result.Nodes)
	}
}
func TestPCI_RejectsEmptyRowBody(t *testing.T) {
	layout, pages := pciFixture()
	pages[41].Blocks[6] = pciBlock(pciWord("78", "165", "1.1.1"))
	n := 3
	if _, err := parsePCIFixture(layout, pages, pciSHA, &n); err == nil {
		t.Fatal("expected empty row rejection")
	}
}
