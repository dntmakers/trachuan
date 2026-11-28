package parse

import (
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestPCI_ContinuationOfEarlierRow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		words []pdftext.Word
		fail  bool
	}{
		{"excluded text", []pdftext.Word{pciWord("78", "200", "1.1.1"), pciWord("110", "200", "(continued)"), pciWord("310", "200", "invented")}, false},
		{"left text", []pdftext.Word{pciWord("78", "200", "1.1.1"), pciWord("110", "200", "(continued)"), pciWord("160", "200", "invented")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, p := pciFixture()
			p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "180", "1.1.2"), pciWord("115", "180", "invented")), pciBlock(tc.words...))
			n := 4
			_, err := parsePCIFixture(l, p, pciSHA, &n)
			if (err != nil) != tc.fail {
				t.Fatalf("error: %v", err)
			}
		})
	}
}

func TestPCI_ReusesBands(t *testing.T) {
	l, p := pciFixture()
	p[42].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciBlock(pciWord("78", "120", "1.1.2"), pciWord("120", "120", "invented")), pciFooter()}
	n := 4
	got, err := parsePCIFixture(l, p, pciSHA, &n)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nodes[3].Citation != "1.1.2" || got.Nodes[3].Parent != 1 {
		t.Fatalf("wrong continuation node: %#v", got.Nodes[3])
	}
}
