package parse

import (
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestPCI_PageEndMultiset(t *testing.T) {
	for _, tc := range []struct {
		name string
		page int
		bad  func() pdftext.Block
		want string
	}{
		{"split rightmost tie", 275, func() pdftext.Block {
			b := pciSplitPageEndCaption("500", "200.04")
			b.Lines[0].Words[2].XMin = "200.04"
			b.Lines[0].Words[2].XMax = "200.04"
			return b
		}, "ambiguous page-end caption"},
		{"split rightmost tie reversed", 275, func() pdftext.Block {
			b := pciSplitPageEndCaption("500", "200.04")
			b.Lines[0].Words[2].XMin = "200.04"
			b.Lines[0].Words[2].XMax = "200.04"
			b.Lines[0].Words[2], b.Lines[0].Words[4] = b.Lines[0].Words[4], b.Lines[0].Words[2]
			return b
		}, "ambiguous page-end caption"},
		{"four-word first tie", 87, func() pdftext.Block {
			b := pciPageEndCaption("500")
			b.Lines[0].Words[1].XMin = "78"
			b.Lines[0].Words[1].XMax = "78"
			return b
		}, "ambiguous page-end caption"},
		{"four-word last tie", 87, func() pdftext.Block {
			b := pciPageEndCaption("500")
			b.Lines[0].Words[2].XMin = "180"
			b.Lines[0].Words[2].XMax = "180"
			return b
		}, "ambiguous page-end caption"},
		{"first endpoint misplaced", 87, func() pdftext.Block {
			b := pciPageEndCaption("500")
			b.Lines[0].Words[0].XMin = "130"
			b.Lines[0].Words[0].XMax = "130"
			return b
		}, "invalid page-end caption"},
		{"last endpoint misplaced", 87, func() pdftext.Block {
			b := pciPageEndCaption("500")
			b.Lines[0].Words[3].XMin = "130"
			b.Lines[0].Words[3].XMax = "130"
			return b
		}, "invalid page-end caption"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, want := pciStrictFixture(true)
			p[tc.page-1].Blocks = append(p[tc.page-1].Blocks, tc.bad())
			pciNormalizeFixture(p)
			n := len(want)
			_, err := parsePCIDSS(p, pciSHA, &n)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s, got %v", tc.want, err)
			}
		})
	}
}
