package fetch

import (
	"strings"
	"testing"
)

func TestVerifyExtractLineageV1BytesReturnsParsedInputs(t *testing.T) {
	hash := strings.Repeat("a", 64)
	data := []byte(`{"text_source":"source_tree","detector_inputs":{"version":1,"ocr_check":{"state":"not_selected"},"tree_transcription":{"state":"no_pair"}}}`)
	source, got, err := VerifyExtractLineageV1Bytes(data, hash, hash)
	if err != nil || source != "source_tree" || got.Version != 1 || got.OCRCheck.State != "not_selected" || got.TreeTranscription.State != "no_pair" {
		t.Fatalf("source=%s inputs=%+v err=%v", source, got, err)
	}
	for _, invalid := range [][]byte{[]byte(`{}`), []byte(`{"text_source":"source_tree","detector_inputs":null}`)} {
		_, _, parsedErr := VerifyExtractLineageV1Bytes(invalid, hash, hash)
		if parsedErr == nil || CheckExtractLineageV1Bytes(invalid, hash, hash) == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
}
