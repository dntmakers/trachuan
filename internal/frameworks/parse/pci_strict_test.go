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
	ranges := [][2]int{{44, 64}, {66, 77}, {79, 113}, {115, 122}, {124, 137}, {139, 164}, {166, 177}, {180, 213}, {215, 239}, {241, 263}, {265, 293}, {295, 337}, {340, 345}, {347, 349}, {352, 372}}
	for _, span := range ranges {
		for page := span[0]; page <= span[1]; page++ {
			pages[page-1].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciFooter()}
		}
	}
	rootPages := []int{42, 65, 78, 114, 123, 138, 165, 178, 214, 240, 264, 294, 339, 346, 350}
	tablePages := []int{44, 66, 79, 115, 124, 139, 166, 180, 215, 241, 265, 295, 340, 347, 352}
	var want []Node
	for i, rootPage := range rootPages {
		id := fmt.Sprint(i + 1)
		rootWord := "Requirement"
		if i >= 12 {
			id = fmt.Sprintf("A%d", i-11)
			rootWord = "Appendix"
		}
		group := id + ".1"
		row := group + ".1"
		pages[rootPage-1].Blocks = []pdftext.Block{pciBlock(pciWord("72", "75", rootWord), pciWord("130", "75", id+":"), pciWord("145", "75", "Invented"))}
		table := tablePages[i] - 1
		pages[table].Blocks = append(pages[table].Blocks, pciBlock(pciWord("78", "125", group), pciWord("110", "125", "Invented")), pciSub("78", "145", true), pciSub("298", "145", false), pciBlock(pciWord("78", "165", row), pciWord("130", "165", "invented")))
		base := len(want)
		body := "invented"
		if i == 2 {
			body += " continued continued"
		}
		if i == 14 && withLast {
			body += " continued"
		}
		want = append(want, Node{Citation: id, CitationNorm: id, Kind: "requirement", Parent: -1, Ordinal: base, PublisherTitle: "Invented", Status: "active"}, Node{Citation: group, CitationNorm: group, Kind: "requirement", Parent: base, Ordinal: base + 1, PublisherTitle: "Invented", Status: "active"}, Node{Citation: row, CitationNorm: row, Kind: "requirement", Parent: base + 1, Ordinal: base + 2, Body: body, Status: "active"})
	}
	for _, page := range []int{96, 98} {
		pages[page-1].Blocks = append(pages[page-1].Blocks, pciBlock(pciWord("78", "120", "(continued)")), pciBlock(pciWord("78", "130", "continued")))
	}
	for _, page := range []int{87, 89, 91, 95, 97, 101, 103, 204, 262, 270, 275, 285, 288, 290, 292, 327, 356, 367} {
		if page == 275 {
			pages[page-1].Blocks = append(pages[page-1].Blocks, pciSplitPageEndCaption("520", "200.04"))
		} else {
			pages[page-1].Blocks = append(pages[page-1].Blocks, pciPageEndCaption("520"))
		}
	}
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
