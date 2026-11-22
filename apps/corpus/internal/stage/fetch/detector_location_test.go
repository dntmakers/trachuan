package fetch

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOCRLocationThroughDecoder(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name  string
		loc   OCRLocation
		valid bool
	}{
		{"earliest", OCRLocation{1, 3, 4, 3, 4}, true},
		{"latest", OCRLocation{1, 3, 4, 8, 9}, true},
		{"before", OCRLocation{1, 3, 4, 2, 3}, false},
		{"inside", OCRLocation{1, 3, 5, 3, 4}, false},
		{"after", OCRLocation{1, 3, 4, 9, 10}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := DetectorInputsV1{Version: 1, TreeTranscription: TreeTranscriptionV1{State: "not_applicable"}, OCRCheck: OCRCheckObservation{State: "checked", OCRCheckDetails: &OCRCheckDetails{PDFSHA256: hash, TextSHA256: hash, ResultSHA256: hash, EngineID: "e", CheckerID: "c", SameDocument: true, Verdict: "flagged", PageCount: 1, ParagraphCount: 5, LineCount: 10, ScopeCount: 2, Items: []OCRCheckItem{{Kind: "article_label", Page: 1, Ordinal: 1, OCRReadingSHA256: hash, PDFReadingSHA256: hash, Position: "tail", Location: &tc.loc}}}}}
			b, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeDetectorInputsV1(b, "")
			if (err == nil) != tc.valid {
				t.Fatalf("err=%v valid=%v", err, tc.valid)
			}
		})
	}
}
