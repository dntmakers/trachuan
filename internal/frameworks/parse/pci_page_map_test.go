package parse

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestPCI_PageKindMap(t *testing.T) {
	spans := [][2]int{{44, 64}, {66, 77}, {79, 113}, {115, 122}, {124, 137}, {139, 164}, {166, 177}, {180, 213}, {215, 239}, {241, 263}, {265, 293}, {295, 337}, {340, 345}, {347, 349}, {352, 372}}
	for page := 42; page <= 372; page++ {
		want := false
		for _, span := range spans {
			if page >= span[0] && page <= span[1] {
				want = true
				break
			}
		}
		got, err := pciPageKind(page, want)
		if err != nil || got != want {
			t.Fatalf("page %d: kind=%v error=%v", page, got, err)
		}
		if _, err = pciPageKind(page, !want); err == nil {
			t.Fatalf("page %d accepted mismatch", page)
		}
	}
	for _, page := range []int{41, 373} {
		if _, err := pciPageKind(page, false); err == nil {
			t.Fatalf("page %d accepted out of range", page)
		}
	}
}

func TestPCI_PageMapEnforcement(t *testing.T) {
	for _, tc := range []struct {
		name        string
		page        int
		remove, add bool
	}{
		{"table page without outer headers", 48, true, false},
		{"overview page with outer headers", 43, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, want := pciStrictFixture(true)
			if tc.remove {
				p[tc.page-1].Blocks = []pdftext.Block{pciBlock(pciWord("78", "120", "invented")), pciFooter()}
			}
			if tc.add {
				p[tc.page-1].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciSub("78", "145", true), pciSub("298", "145", false), pciFooter()}
			}
			pciNormalizeFixture(p)
			n := len(want)
			_, err := parsePCIDSS(p, pciSHA, &n)
			if err == nil || !strings.Contains(err.Error(), "page kind") {
				t.Fatalf("expected page kind error, got %v", err)
			}
		})
	}
}

func TestPCI_PinnedCaptionEnforcement(t *testing.T) {
	for _, page := range []int{87, 89, 91, 95, 97, 101, 103, 204, 262, 270, 275, 285, 288, 290, 292, 327, 356, 367} {
		t.Run("missing pinned caption "+strconv.Itoa(page), func(t *testing.T) {
			p, want := pciStrictFixture(true)
			blocks := p[page-1].Blocks
			p[page-1].Blocks = blocks[:len(blocks)-1]
			n := len(want)
			_, err := parsePCIDSS(p, pciSHA, &n)
			if err == nil || !strings.Contains(err.Error(), "missing page-end caption") {
				t.Fatalf("expected missing caption error, got %v", err)
			}
		})
	}
	t.Run("overview caption ignored", func(t *testing.T) {
		p, want := pciStrictFixture(true)
		p[349].Blocks = append(p[349].Blocks, pciPageEndCaption("520"))
		pciNormalizeFixture(p)
		n := len(want)
		got, err := parsePCIDSS(p, pciSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Nodes, want) {
			t.Fatalf("unexpected nodes: %#v", got.Nodes)
		}
	})
	t.Run("lookalike does not satisfy pinned caption", func(t *testing.T) {
		p, want := pciStrictFixture(true)
		p[86].Blocks[len(p[86].Blocks)-1] = pciBlock(pciWord("78", "520", "(continued"), pciWord("110", "520", "one"), pciWord("130", "520", "two"), pciWord("150", "520", "three"), pciWord("180", "520", "page)"))
		pciNormalizeFixture(p)
		n := len(want)
		_, err := parsePCIDSS(p, pciSHA, &n)
		if err == nil || !strings.Contains(err.Error(), "missing page-end caption") {
			t.Fatalf("expected missing caption, got %v", err)
		}
	})
	t.Run("unexpected caption", func(t *testing.T) {
		p, want := pciStrictFixture(true)
		p[87].Blocks = append(p[87].Blocks, pciPageEndCaption("520"))
		pciNormalizeFixture(p)
		n := len(want)
		_, err := parsePCIDSS(p, pciSHA, &n)
		if err == nil || !strings.Contains(err.Error(), "page-end caption") {
			t.Fatalf("expected caption error, got %v", err)
		}
	})
	for _, tc := range []struct {
		name        string
		left, right int
		reverse     bool
	}{
		{"split first tie", 0, 1, false}, {"split first tie reversed", 0, 1, true},
		{"split middle tie", 1, 2, false}, {"split middle tie reversed", 1, 2, true},
		{"split page tie", 2, 3, false}, {"split page tie reversed", 2, 3, true},
		{"split final tie", 3, 4, false}, {"split final tie reversed", 3, 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, want := pciStrictFixture(true)
			b := pciSplitPageEndCaption("520", "200.04")
			words := b.Lines[0].Words
			words[tc.left].XMin = words[tc.right].XMin
			words[tc.left].XMax = words[tc.right].XMax
			if tc.reverse {
				words[tc.left], words[tc.right] = words[tc.right], words[tc.left]
			}
			p[274].Blocks[len(p[274].Blocks)-1] = b
			pciNormalizeFixture(p)
			n := len(want)
			_, err := parsePCIDSS(p, pciSHA, &n)
			if err == nil || !strings.Contains(err.Error(), "ambiguous page-end caption") {
				t.Fatalf("expected ambiguous caption error, got %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name        string
		left, right int
		reverse     bool
	}{
		{"first tie", 0, 1, false}, {"first tie reversed", 0, 1, true},
		{"middle tie", 1, 2, false}, {"middle tie reversed", 1, 2, true},
		{"last tie", 2, 3, false}, {"last tie reversed", 2, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, want := pciStrictFixture(true)
			b := pciPageEndCaption("520")
			words := b.Lines[0].Words
			words[tc.left].XMin = words[tc.right].XMin
			words[tc.left].XMax = words[tc.right].XMax
			if tc.reverse {
				words[tc.left], words[tc.right] = words[tc.right], words[tc.left]
			}
			p[86].Blocks[len(p[86].Blocks)-1] = b
			pciNormalizeFixture(p)
			n := len(want)
			_, err := parsePCIDSS(p, pciSHA, &n)
			if err == nil || !strings.Contains(err.Error(), "ambiguous page-end caption") {
				t.Fatalf("expected ambiguous caption error, got %v", err)
			}
		})
	}
}
