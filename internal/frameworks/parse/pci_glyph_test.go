package parse

import "testing"

func TestPCIBody_AttachesGlyphAboveOrBelow(t *testing.T) {
	for _, tc := range []struct {
		name          string
		glyphY, textY float64
	}{{"above", 20, 21}, {"below", 21, 20}} {
		t.Run(tc.name, func(t *testing.T) {
			body := pciBody{lines: []pciBodyLine{{page: 1, block: 1, key: pciKey{x: 78, y: tc.glyphY}, geometry: pciBox{xmin: 78, ymin: tc.glyphY}, text: "•"}, {page: 1, block: 2, key: pciKey{x: 90, y: tc.textY}, geometry: pciBox{xmin: 90, ymin: tc.textY}, text: "invented"}}}
			got, err := body.text()
			if err != nil || got != "• invented" {
				t.Fatalf("got %q error %v", got, err)
			}
		})
	}
}
