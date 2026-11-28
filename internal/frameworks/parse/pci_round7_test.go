package parse

import (
	"strings"
	"testing"
)

func TestPCIBody_GlyphMatchingKeepsReadingOrder(t *testing.T) {
	body := pciBody{}
	glyph := func(id string, anchor, position float64, block int) pciLine {
		return pciLine{role: pciLineRow, citation: id, lineTop: anchor, retainedTop: anchor, block: block, retained: []pciProjectedWord{{text: id, box: pciBox{xmin: 78, ymin: anchor}}, {text: "•", box: pciBox{xmin: 125, ymin: position}, emit: true}}}
	}
	text := func(value string, y float64, block int) pciLine {
		return pciLine{role: pciLineBody, lineTop: y, retainedTop: y, block: block, retained: []pciProjectedWord{{text: value, box: pciBox{xmin: 150, ymin: y}, emit: true}}}
	}
	body.add(glyph("1.1.1", 150, 170, 1), 0)
	body.add(glyph("1.1.2", 160, 160, 2), 0)
	body.add(text("second", 161, 3), 0)
	body.add(text("first", 171, 4), 0)
	got, err := body.text()
	if err != nil || got != "• second\n• first" {
		t.Fatalf("unexpected body %q, error %v", got, err)
	}
}

func TestPCIBody_GlyphUsesEmittedGeometry(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		textX, glyphY, textY float64
		wantErr              bool
	}{
		{"text left of glyph", 100, 165, 166, true},
		{"text right of glyph", 150, 165, 166, false},
		{"text distant from glyph", 150, 170, 166, true},
		{"text near glyph", 150, 170, 171, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := pciBody{}
			glyph := pciLine{role: pciLineRow, citation: "1.1.1", lineTop: 165, retainedTop: 165, block: 1, retained: []pciProjectedWord{{text: "1.1.1", box: pciBox{xmin: 78, ymin: 165}}, {text: "•", box: pciBox{xmin: 125, ymin: tc.glyphY}, emit: true}}}
			text := pciLine{role: pciLineBody, lineTop: tc.textY, retainedTop: tc.textY, block: 2, retained: []pciProjectedWord{{text: "invented", box: pciBox{xmin: tc.textX, ymin: tc.textY}, emit: true}}}
			body.add(glyph, 0)
			body.add(text, 0)
			got, err := body.text()
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "ambiguous list glyph") {
					t.Fatalf("expected glyph error, got %q, %v", got, err)
				}
				return
			}
			if err != nil || got != "• invented" {
				t.Fatalf("unexpected body %q, error %v", got, err)
			}
		})
	}
}
