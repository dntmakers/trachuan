package parse

import (
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func pciPageEndCaption(y string) pdftext.Block {
	return pciBlock(pciWord("78", y, "(continued"), pciWord("120", y, "invented"), pciWord("145", y, "invented"), pciWord("180", y, "page)"))
}

func pciSplitPageEndCaption(y string, gap string) pdftext.Block {
	page := pciWord("180", y, "page")
	page.XMax = "200"
	close := pciWord(gap, y, ")")
	return pciBlock(pciWord("78", y, "(continued"), pciWord("120", y, "invented"), pciWord("145", y, "invented"), page, close)
}

func TestPCI_PageEndRoleIndependence(t *testing.T) {
	t.Run("group opener contains caption shape", func(t *testing.T) {
		l, p := pciFixture()
		p[41].Blocks[3] = pciBlock(pciWord("78", "125", "1.1"), pciWord("74", "125", "(continued"), pciWord("120", "125", "invented"), pciWord("150", "125", "page)"))
		n := 3
		_, err := parsePCIFixture(l, p, pciSHA, &n)
		if err == nil || !strings.Contains(err.Error(), "page-end caption") {
			t.Fatalf("expected caption error, got %v", err)
		}
	})
	t.Run("row opener contains caption shape", func(t *testing.T) {
		l, p := pciFixture()
		p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "180", "1.1.2"), pciWord("74", "180", "(continued"), pciWord("120", "180", "invented"), pciWord("150", "180", "page)")))
		n := 4
		_, err := parsePCIFixture(l, p, pciSHA, &n)
		if err == nil || !strings.Contains(err.Error(), "page-end caption") {
			t.Fatalf("expected caption error, got %v", err)
		}
	})
	t.Run("five-word lookalike remains body", func(t *testing.T) {
		l, p := pciFixture()
		p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "180", "(continued"), pciWord("110", "180", "one"), pciWord("130", "180", "two"), pciWord("150", "180", "three"), pciWord("180", "180", "page)")))
		n := 3
		r, err := parsePCIFixture(l, p, pciSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		want := pciWantNodes("Invented\n\n(continued one two three page)")
		if !reflect.DeepEqual(r.Nodes, want) {
			t.Fatalf("unexpected nodes: %#v", r.Nodes)
		}
	})
	t.Run("interior closing parenthesis remains body", func(t *testing.T) {
		l, p := pciFixture()
		p[41].Blocks = append(p[41].Blocks, pciBlock(pciWord("78", "180", "(continued"), pciWord("120", "180", "invented"), pciWord("145", "180", ")"), pciWord("160", "180", "applies")))
		n := 3
		r, err := parsePCIFixture(l, p, pciSHA, &n)
		if err != nil {
			t.Fatal(err)
		}
		want := pciWantNodes("Invented\n\n(continued invented ) applies")
		if !reflect.DeepEqual(r.Nodes, want) {
			t.Fatalf("unexpected nodes: %#v", r.Nodes)
		}
	})
}

func TestPCI_RejectsSubheadersWithoutOuterHeader(t *testing.T) {
	p, want := pciStrictFixture(true)
	p[47].Blocks = []pdftext.Block{pciSub("78", "145", true), pciSub("298", "145", false), pciBlock(pciWord("78", "165", "invented")), pciFooter()}
	pciNormalizeFixture(p)
	n := len(want)
	_, err := parsePCIDSS(p, pciSHA, &n)
	if err == nil || !strings.Contains(err.Error(), "page kind") {
		t.Fatalf("expected page kind error, got %v", err)
	}
}

func TestPCI_RejectsMalformedSplitCaption(t *testing.T) {
	l, p := pciFixture()
	bad := pciSplitPageEndCaption("520", "200.04")
	bad.Lines[0].Words[3].Text = "invented"
	p[274].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), bad, pciFooter()}
	n := 3
	_, err := parsePCIFixture(l, p, pciSHA, &n)
	if err == nil || !strings.Contains(err.Error(), "page-end caption") {
		t.Fatalf("expected page-end caption error, got %v", err)
	}
}

func TestPCI_SplitPageEndCaption(t *testing.T) {
	for _, tc := range []struct {
		name    string
		page    int
		closeX  string
		wantErr bool
	}{
		{"pinned adjacent split", 275, "200.04", false},
		{"split on another page", 43, "200.04", true},
		{"nonadjacent split", 275, "200.20", true},
		{"reversed split", 275, "179", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, p := pciFixture()
			p[tc.page-1].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciSplitPageEndCaption("520", tc.closeX), pciFooter()}
			n := 3
			r, err := parsePCIFixture(l, p, pciSHA, &n)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "page-end caption") {
					t.Fatalf("expected page-end caption error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r.Nodes, pciWantNodes("Invented")) {
				t.Fatalf("unexpected nodes: %#v", r.Nodes)
			}
		})
	}
}

func TestPCI_PageEndCaption(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func([]pdftext.Page)
		wantErr string
	}{
		{"caption shares excluded cell", func(p []pdftext.Page) {
			b := pciPageEndCaption("520")
			b.Lines[0].Words = append(b.Lines[0].Words, pciWord("310", "520", "invented"))
			p[86].Blocks = append(p[86].Blocks, b)
		}, ""},
		{"caption overlaps spanning heading", func(p []pdftext.Page) {
			b := pciPageEndCaption("520")
			b.Lines[0].Words = append(b.Lines[0].Words, pciWord("310", "520", "invented"))
			p[41].Blocks[3].Lines = append(p[41].Blocks[3].Lines, b.Lines[0])
		}, "heading"},
		{"caption in heading block", func(p []pdftext.Page) {
			p[41].Blocks[3].Lines = append(p[41].Blocks[3].Lines, pciPageEndCaption("520").Lines[0])
		}, "heading"},
		{"overview outside table", func(p []pdftext.Page) { p[349].Blocks = []pdftext.Block{pciPageEndCaption("520"), pciFooter()} }, ""},
		{"ambiguous reversed word order", func(p []pdftext.Page) {
			b := pciPageEndCaption("520")
			b.Lines[0].Words[1].XMin = "78"
			b.Lines[0].Words[1].XMax = "78"
			b.Lines[0].Words[0], b.Lines[0].Words[1] = b.Lines[0].Words[1], b.Lines[0].Words[0]
			p[86].Blocks = append(p[86].Blocks, b)
		}, "page-end caption"},
		{"ambiguous word order", func(p []pdftext.Page) {
			b := pciPageEndCaption("520")
			b.Lines[0].Words[1].XMin = "78"
			b.Lines[0].Words[1].XMax = "78"
			p[86].Blocks = append(p[86].Blocks, b)
		}, "page-end caption"},
		{"caption above table", func(p []pdftext.Page) { p[86].Blocks = append(p[86].Blocks, pciPageEndCaption("80")) }, "page-end caption"},
		{"caption crosses footer", func(p []pdftext.Page) {
			b := pciPageEndCaption("520")
			b.Lines[0].Words[3].YMin = "540"
			b.Lines[0].Words[3].YMax = "540"
			p[86].Blocks = append(p[86].Blocks, b)
		}, "page-end caption"},
		{"drop last left line", func(p []pdftext.Page) { p[86].Blocks = append(p[86].Blocks, pciPageEndCaption("520")) }, ""},
		{"later left text", func(p []pdftext.Page) {
			p[86].Blocks = append(p[86].Blocks, pciPageEndCaption("500"), pciBlock(pciWord("78", "520", "invented")))
		}, "page-end caption"},
		{"mid-page before opener", func(p []pdftext.Page) {
			p[86].Blocks = append(p[86].Blocks, pciPageEndCaption("170"), pciBlock(pciWord("78", "180", "1.1.2"), pciWord("120", "180", "invented")))
		}, "page-end caption"},
		{"later paired subheader", func(p []pdftext.Page) {
			p[86].Blocks = append(p[86].Blocks, pciPageEndCaption("500"), pciSub("78", "520", true), pciSub("298", "520", false))
		}, "page-end caption"},
		{"later excluded column", func(p []pdftext.Page) {
			p[86].Blocks = append(p[86].Blocks, pciPageEndCaption("520"), pciBlock(pciWord("310", "530", "invented")))
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, p := pciFixture()
			if tc.name != "caption overlaps spanning heading" && tc.name != "caption in heading block" && tc.name != "overview outside table" {
				p[86].Blocks = []pdftext.Block{pciOuter(), pciBlock(pciWord("590", "90", "Guidance")), pciFooter()}
			}
			tc.change(p)
			n := 3
			r, err := parsePCIFixture(l, p, pciSHA, &n)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected %s, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r.Nodes, pciWantNodes("Invented")) {
				t.Fatalf("unexpected nodes: %#v", r.Nodes)
			}
		})
	}
}
