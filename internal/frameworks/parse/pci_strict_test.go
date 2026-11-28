package parse

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func pciStrictFixture(withLast bool) ([]pdftext.Page, []Node) {
	pages := make([]pdftext.Page, 372)
	var want []Node
	for i := 0; i < 15; i++ {
		id := fmt.Sprint(i + 1)
		page := 41 + i
		rootWord := "Requirement"
		if i >= 12 {
			id = fmt.Sprintf("A%d", i-11)
			rootWord = "Appendix"
		}
		if i == 14 {
			page = 370
		}
		group := id + ".1"
		row := group + ".1"
		pages[page].Blocks = []pdftext.Block{pciBlock(pciWord("72", "75", rootWord), pciWord("130", "75", id+":"), pciWord("145", "75", "Invented")), pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciBlock(pciWord("78", "125", group), pciWord("110", "125", "Invented")), pciSub("78", "145", true), pciSub("298", "145", false), pciBlock(pciWord("78", "165", row), pciWord("130", "165", "invented")), pciFooter()}
		base := len(want)
		body := "invented"
		if i == 13 {
			body += " continued continued"
		}
		if i == 14 && withLast {
			body += " continued"
		}
		want = append(want, Node{Citation: id, CitationNorm: id, Kind: "requirement", Parent: -1, Ordinal: base, PublisherTitle: "Invented", Status: "active"}, Node{Citation: group, CitationNorm: group, Kind: "requirement", Parent: base, Ordinal: base + 1, PublisherTitle: "Invented", Status: "active"}, Node{Citation: row, CitationNorm: row, Kind: "requirement", Parent: base + 1, Ordinal: base + 2, Body: body, Status: "active"})
	}
	for _, page := range []int{95, 97} {
		pages[page].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciBlock(pciWord("78", "120", "(continued)")), pciBlock(pciWord("78", "130", "continued")), pciFooter()}
	}
	pages[371].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciFooter()}
	if withLast {
		pages[371].Blocks = append(pages[371].Blocks, pciBlock(pciWord("78", "120", "continued")))
	}
	pciNormalizeFixture(pages)
	return pages, want
}
func TestPCI_StrictEntry(t *testing.T) {
	t.Run("complete", func(t *testing.T) {
		p, want := pciStrictFixture(true)
		n := len(want)
		got, err := parsePCIDSS(p, pciSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Nodes, want) {
			t.Fatalf("nodes mismatch: %#v", got.Nodes)
		}
	})
	t.Run("empty last table", func(t *testing.T) {
		p, want := pciStrictFixture(false)
		n := len(want)
		_, err := parsePCIDSS(p, pciSHA, &n)
		if err == nil || !strings.Contains(err.Error(), "missing required region") {
			t.Fatalf("expected missing region, got %v", err)
		}
	})
}
