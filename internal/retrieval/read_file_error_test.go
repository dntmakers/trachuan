package retrieval

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReadFiles_ErrorRefundsPreviewForNextItem(t *testing.T) {
	firstDoc := &DocumentEntry{Title: "First"}
	nextDoc := &DocumentEntry{Title: strings.Repeat("x", 400)}
	first := readSequence{doc: "vn:1", document: firstDoc, files: []SourceFile{{Name: "file", Hash: strings.Repeat("a", 64), Role: "main"}}, units: []Unit{{Kind: "unknown", Text: strings.Repeat("x", PartSize+1)}}}
	second := readSequence{doc: "vn:2", document: nextDoc, complete: true}
	budget := readSize(firstDoc) + readSize(nextDoc)
	result, used, err := readPage(first, cursor{}, budget, true)
	if err != nil || result.Error != "unit_too_large" {
		t.Fatalf("first: %+v %v", result, err)
	}
	next, _, err := readPage(second, cursor{}, budget-used, false)
	if err != nil || next.Document == nil || next.NotRead != "" {
		t.Fatalf("second: %+v, used %d, err %v", next, used, err)
	}
}

func TestReadFiles_ErrorOmitsFiles(t *testing.T) {
	seq := readSequence{doc: "vn:1", document: &DocumentEntry{Title: "Law"}, files: []SourceFile{{Name: "file", Hash: strings.Repeat("a", 64), Role: "main"}}, units: []Unit{{Kind: "unknown", Text: strings.Repeat("x", PartSize+1)}}}
	result, _, err := readPage(seq, cursor{}, ReadCallCap, true)
	raw, marshalErr := json.Marshal(result)
	if err != nil || marshalErr != nil || result.Error != "unit_too_large" || strings.Contains(string(raw), `"files`) {
		t.Fatalf("result %s, error %v, marshal %v", raw, err, marshalErr)
	}
}
