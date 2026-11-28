package parse

import (
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestPCI_StandaloneCaption(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape string
		fail  bool
	}{
		{"first left block", "first", false},
		{"between continuation text", "between", false},
		{"paired subheader", "paired", true},
		{"after opener", "opener", true},
		{"no active row", "inactive", true},
		{"duplicate marker", "duplicate", true},
		{"missing following text", "no-follow", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, p := pciFixture()
			if tc.shape == "inactive" {
				p[41].Blocks = p[41].Blocks[:6]
				p[41].Blocks = append(p[41].Blocks, pciFooter())
			}
			p[95].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciBlock(pciWord("78", "120", "(continued)")), pciFooter()}
			switch tc.shape {
			case "first":
				p[95].Blocks = append(p[95].Blocks, pciBlock(pciWord("78", "130", "after")))
			case "duplicate":
				p[95].Blocks = append(p[95].Blocks, pciBlock(pciWord("78", "130", "invented")), pciBlock(pciWord("78", "140", "(continued)")), pciBlock(pciWord("78", "150", "invented")))
			case "between":
				p[95].Blocks = append(p[95].Blocks, pciBlock(pciWord("78", "110", "before")), pciBlock(pciWord("78", "130", "after")))
			case "paired":
				p[95].Blocks = append(p[95].Blocks, pciSub("78", "105", true), pciSub("298", "105", false))
			case "opener":
				p[95].Blocks = append(p[95].Blocks, pciBlock(pciWord("78", "110", "1.1.2"), pciWord("120", "110", "invented")))
			}
			n := 3
			if tc.shape == "inactive" {
				n = 2
			}
			if tc.shape == "opener" {
				n = 4
			}
			got, err := parsePCIFixture(l, p, pciSHA, &n)
			if tc.fail {
				want := "invalid standalone caption"
				if tc.shape == "no-follow" {
					want = "missing standalone continuation"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("expected %s, got %v", want, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !tc.fail {
				body := "Invented after"
				if tc.shape == "between" {
					body = "Invented before\n\nafter"
				}
				if !reflect.DeepEqual(got.Nodes, pciWantNodes(body)) {
					t.Fatalf("unexpected nodes: %#v", got.Nodes)
				}
			}
		})
	}
}
