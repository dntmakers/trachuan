package parse

import (
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestPCI_ReviewRound4(t *testing.T) {
	t.Run("id-only opener retains nonemitting anchor", func(t *testing.T) {
		l, p := pciFixture()
		p[42].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciBlock(pciWord("78", "165", "1.1.2")), pciBlock(pciWord("78", "175", "after")), pciFooter()}
		pciNormalizeFixture(p)
		lines, err := pciClassifyPage(p[41])
		if err != nil {
			t.Fatal(err)
		}
		first, _, err := pciHeaderForLines(lines, nil)
		if err != nil {
			t.Fatal(err)
		}
		lines, err = pciClassifyPage(p[42])
		if err != nil {
			t.Fatal(err)
		}
		h, _, err := pciHeaderForLines(lines, map[[2]float64]pciHeader{{first.outerLeft, first.outerRight}: first})
		if err != nil {
			t.Fatal(err)
		}
		if err = pciClassifyTable(lines, h, true, 43); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, line := range lines {
			if line.role == pciLineRow {
				found = true
				if len(line.retained) != 1 || line.retained[0].emit || line.retained[0].box.ymin != 165 {
					t.Fatalf("invalid opener anchor: %#v", line.retained)
				}
			}
		}
		if !found {
			t.Fatal("missing opener")
		}
		n := 4
		r, err := parsePCIFixture(l, p, pciSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		want := append(pciWantNodes("Invented"), Node{Citation: "1.1.2", CitationNorm: "1.1.2", Kind: "requirement", Parent: 1, Ordinal: 3, Body: "after", Status: "active"})
		if !reflect.DeepEqual(r.Nodes, want) {
			t.Fatalf("unexpected nodes: %#v", r.Nodes)
		}
	})
	t.Run("whitespace does not anchor retained geometry", func(t *testing.T) {
		l, p := pciFixture()
		p[42].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciBlock(pciWord("310", "85", "excluded"), pciWord("78", "100", "   "), pciWord("78", "190", "later")), pciBlock(pciWord("78", "165", "1.1.2"), pciWord("120", "165", "next")), pciFooter()}
		n := 4
		r, err := parsePCIFixture(l, p, pciSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		want := append(pciWantNodes("Invented"), Node{Citation: "1.1.2", CitationNorm: "1.1.2", Kind: "requirement", Parent: 1, Ordinal: 3, Body: "next\n\nlater", Status: "active"})
		if !reflect.DeepEqual(r.Nodes, want) {
			t.Fatalf("unexpected nodes: %#v", r.Nodes)
		}
	})
	t.Run("retained geometry owns continuation", func(t *testing.T) {
		l, p := pciFixture()
		p[42].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciBlock(pciWord("310", "85", "excluded"), pciWord("78", "190", "later")), pciBlock(pciWord("78", "165", "1.1.2"), pciWord("120", "165", "next")), pciFooter()}
		n := 4
		r, err := parsePCIFixture(l, p, pciSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		want := append(pciWantNodes("Invented"), Node{Citation: "1.1.2", CitationNorm: "1.1.2", Kind: "requirement", Parent: 1, Ordinal: 3, Body: "next\n\nlater", Status: "active"})
		if !reflect.DeepEqual(r.Nodes, want) {
			t.Fatalf("unexpected nodes: %#v", r.Nodes)
		}
	})
	t.Run("outer label maximum extent", func(t *testing.T) {
		_, p := pciFixture()
		p[41].Blocks[1].Lines[0].Words[1].XMax = "350"
		h, ok, err := pciHeaderForPage(p[41], nil)
		if err != nil || !ok || h.right != 466 {
			t.Fatalf("right=%v table=%v error=%v", h.right, ok, err)
		}
	})
	t.Run("crossing maximum extent", func(t *testing.T) {
		l, p := pciFixture()
		p[41].Blocks[1].Lines[0].Words[1].XMax = "350"
		p[41].Blocks = append(p[41].Blocks, pciBlock(pdftext.Word{XMin: "465", XMax: "467", YMin: "180", YMax: "180", Text: "invented"}))
		n := 3
		_, err := parsePCIFixture(l, p, pciSHA, &n)
		if err == nil || !strings.Contains(err.Error(), "crossing table band") {
			t.Fatalf("expected crossing error, got %v", err)
		}
	})
	t.Run("section outside label window", func(t *testing.T) {
		l, p := pciFixture()
		p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "531", "Applicability"), pciWord("150", "531", "Notes"), pciWord("180", "531", "invented")))
		n := 3
		r, err := parsePCIFixture(l, p, pciSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		want := pciWantNodes("Invented\n\nApplicability Notes invented")
		if !reflect.DeepEqual(r.Nodes, want) {
			t.Fatalf("unexpected nodes: %#v", r.Nodes)
		}
	})
}
