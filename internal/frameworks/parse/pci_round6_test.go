package parse

import (
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestPCI_ReviewRound6(t *testing.T) {
	t.Run("words on one line follow x order", func(t *testing.T) {
		l, p := pciFixture()
		p[41].Blocks[6] = pciBlock(pciWord("78", "165.363", "1.1.1"), pciWord("110", "165.363", "first"), pciWord("150", "165.000", "second"))
		p[41].Blocks[6].Lines[0].YMin = "165.000"
		n := 3
		r, err := parsePCIFixture(l, p, pciSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		want := pciWantNodes("first second")
		if !reflect.DeepEqual(r.Nodes, want) {
			t.Fatalf("unexpected nodes: %#v", r.Nodes)
		}
	})
	t.Run("body output follows retained line key", func(t *testing.T) {
		l, p := pciFixture()
		later := pciBlock(pciWord("78", "180", "later"))
		later.Lines[0].YMin = "185"
		p[41].Blocks = append(p[41].Blocks, later, pciBlock(pciWord("78", "182", "earlier")))
		n := 3
		r, err := parsePCIFixture(l, p, pciSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		want := pciWantNodes("Invented\n\nearlier\n\nlater")
		if !reflect.DeepEqual(r.Nodes, want) {
			t.Fatalf("unexpected nodes: %#v", r.Nodes)
		}
	})
	t.Run("walker uses retained line key", func(t *testing.T) {
		l, p := pciFixture()
		row := pciBlock(pciWord("78", "180", "1.1.2"), pciWord("120", "180", "next"))
		row.Lines[0].YMin = "185"
		p[42].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), row, pciBlock(pciWord("78", "182", "before")), pciFooter()}
		n := 4
		r, err := parsePCIFixture(l, p, pciSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		want := append(pciWantNodes("Invented before"), Node{Citation: "1.1.2", CitationNorm: "1.1.2", Kind: "requirement", Parent: 1, Ordinal: 3, Body: "next", Status: "active"})
		if !reflect.DeepEqual(r.Nodes, want) {
			t.Fatalf("unexpected nodes: %#v", r.Nodes)
		}
	})
	t.Run("group title after next row", func(t *testing.T) {
		l, p := pciFixture()
		p[41].Blocks[3].Lines[0].Words = append(p[41].Blocks[3].Lines[0].Words, pciWord("150", "190", "invented"))
		n := 3
		_, err := parsePCIFixture(l, p, pciSHA, &n)
		if err == nil || !strings.Contains(err.Error(), "ownership") {
			t.Fatalf("expected ownership error, got %v", err)
		}
	})
	t.Run("caption at next opener baseline", func(t *testing.T) {
		l, p := pciFixture()
		p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("77", "180", "1.1.1"), pciWord("78", "180", "(continued)"), pciWord("79", "180", "invented")), pciBlock(pciWord("80", "180", "1.1.2"), pciWord("120", "180", "next")))
		n := 4
		_, err := parsePCIFixture(l, p, pciSHA, &n)
		if err == nil || !strings.Contains(err.Error(), "ownership") {
			t.Fatalf("expected ownership error, got %v", err)
		}
	})
}
