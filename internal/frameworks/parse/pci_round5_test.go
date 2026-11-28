package parse

import (
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestPCI_RetainedLineTopOwnership(t *testing.T) {
	lines := []pciLine{
		{role: pciLineBody, owner: 3, lineTop: 85, retainedTop: 190, retained: []pciProjectedWord{{text: "invented", box: pciBox{xmin: 78, ymin: 190}, emit: true}}},
		{role: pciLineRow, owner: 3, citation: "1.1.2", lineTop: 165, retainedTop: 165, retained: []pciProjectedWord{{text: "1.1.2", box: pciBox{xmin: 78, ymin: 165}, emit: false}}},
	}
	if err := pciValidateWordOwnership(lines, 2); err != nil {
		t.Fatalf("unexpected ownership error: %v", err)
	}
}

func TestPCI_LineBoxOwnership(t *testing.T) {
	t.Run("same line above ID", func(t *testing.T) {
		l, p := pciFixture()
		p[41].Blocks[6] = pciBlock(pciWord("78", "165.363", "1.1.1"), pciWord("120", "165.000", "invented"))
		p[41].Blocks[6].Lines[0].YMin = "165.000"
		n := 3
		r, err := parsePCIFixture(l, p, pciSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(r.Nodes, pciWantNodes("invented")) {
			t.Fatalf("unexpected nodes: %#v", r.Nodes)
		}
	})
	t.Run("separate line above ID", func(t *testing.T) {
		l, p := pciFixture()
		p[41].Blocks[6] = pdftext.Block{Lines: []pdftext.Line{{YMin: "165.363", Words: []pdftext.Word{pciWord("78", "165.363", "1.1.1")}}, {YMin: "165.000", Words: []pdftext.Word{pciWord("120", "165.000", "invented")}}}}
		n := 3
		_, err := parsePCIFixture(l, p, pciSHA, &n)
		if err == nil || !strings.Contains(err.Error(), "ownership") {
			t.Fatalf("expected ownership error, got %v", err)
		}
	})
}

func TestPCI_RejectsInvalidLineTop(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "invalid"} {
		t.Run(value, func(t *testing.T) {
			l, p := pciFixture()
			p[41].Blocks[6].Lines[0].YMin = value
			n := 3
			_, err := parsePCIFixture(l, p, pciSHA, &n)
			if err == nil || !strings.Contains(err.Error(), "invalid line coordinates") {
				t.Fatalf("expected invalid line coordinates, got %v", err)
			}
		})
	}
}

func TestPCI_OpenerWordOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, y string
		fail    bool
	}{
		{"word after next opener", "190", true},
		{"word at next opener", "180", true},
		{"word before next opener", "170", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, p := pciFixture()
			p[41].Blocks[6].Lines[0].Words = append(p[41].Blocks[6].Lines[0].Words, pciWord("150", tc.y, "invented"))
			p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "180", "1.1.2"), pciWord("120", "180", "next")))
			n := 4
			got, err := parsePCIFixture(l, p, pciSHA, &n)
			if tc.fail {
				if err == nil || !strings.Contains(err.Error(), "ownership") {
					t.Fatalf("expected ownership error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := append(pciWantNodes("Invented invented"), Node{Citation: "1.1.2", CitationNorm: "1.1.2", Kind: "requirement", Parent: 1, Ordinal: 3, Body: "next", Status: "active"})
			if !reflect.DeepEqual(got.Nodes, want) {
				t.Fatalf("unexpected nodes: %#v", got.Nodes)
			}
		})
	}
}
