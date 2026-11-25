package retrieval

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReadPageSourceFiles(t *testing.T) {
	seq := readSequence{doc: "vn:1", document: &DocumentEntry{Title: "Law"}, files: []SourceFile{{Name: "law.pdf", Hash: strings.Repeat("a", 64), Role: "main"}}, complete: true}
	result, _, err := readPage(seq, cursor{}, ReadCallCap, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"files":[`) || !strings.Contains(string(raw), `"url":null`) || !strings.Contains(string(raw), `"fetched_at":null`) {
		t.Fatalf("source file missing: %s", raw)
	}
}

func TestReadPageSourceFileContinuation(t *testing.T) {
	seq := readSequence{doc: "vn:1", document: &DocumentEntry{Title: "Law"}, files: []SourceFile{{Name: "file", Hash: strings.Repeat("a", 64), Role: "main"}}, units: []Unit{{Kind: "outline", Heading: "one"}, {Kind: "outline", Heading: "two"}}, complete: true}
	c := cursor{V: 1, Release: "20991110_1", Item: ReadItem{Handle: "vn:1"}, Unit: 1}
	item, _, err := readPage(seq, c, ReadCallCap, true)
	raw, marshalErr := json.Marshal(item)
	if err != nil || marshalErr != nil || strings.Contains(string(raw), `"files`) {
		t.Fatalf("continuation: %s %v %v", raw, err, marshalErr)
	}
}

func TestReadPageEmptySourceFiles(t *testing.T) {
	seq := readSequence{doc: "vn:1", document: &DocumentEntry{Title: "Law"}, complete: true}
	for _, whole := range []bool{false, true} {
		item, _, err := readPage(seq, cursor{WholeDocument: whole}, ReadCallCap, true)
		if err != nil || item.FilesPresent != whole || (whole && item.Files == nil) {
			t.Fatalf("whole %v: %+v %v", whole, item, err)
		}
	}
}
