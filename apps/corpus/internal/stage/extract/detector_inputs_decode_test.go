package extract

import (
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

func writerDetectorFixture(t *testing.T, full bool) []byte {
	t.Helper()
	selected := selection{source: "source_tree", ocrState: "not_selected", treeTranscription: treeTranscriptionObservation{State: "no_pair"}}
	var text []byte
	if full {
		check := ocrstore.CheckResult{SameDocument: true, Pages: 1, PDFPages: 1, Verdict: "flagged", Items: []ocrstore.CheckItem{{Kind: "article_label", Page: 1, OCR: "Điều 5", PDF: "Điều 6"}}}
		selected = observationSelection(t, check, locatorFixture())
		selected.source = "source_tree"
		selected.treeTranscription = treeTranscriptionObservation{State: "measured", TreeChars: 3, TranscriptionChars: 5, TreeTextSHA256: strings.Repeat("a", 64), TranscriptionTextSHA256: strings.Repeat("b", 64), TranscriptionFileSHA256: strings.Repeat("c", 64), MainAccepted: false}
		text = []byte(locatorFixture())
	}
	encoded, err := marshalV1TextRecord(nil, selected, text)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &record); err != nil {
		t.Fatal(err)
	}
	return record["detector_inputs"]
}

func TestDetectorInputsDecoderWriterRoundTrip(t *testing.T) {
	for _, full := range []bool{false, true} {
		input := writerDetectorFixture(t, full)
		got, err := fetch.DecodeDetectorInputsV1(input, "source_tree")
		if err != nil || got.Version != 1 {
			t.Fatalf("full=%v record=%s got=%+v err=%v", full, input, got, err)
		}
		encoded, err := json.Marshal(got)
		if err != nil || string(encoded) != string(input) {
			t.Fatalf("input=%s encoded=%s err=%v", input, encoded, err)
		}
	}
}

func TestDetectorInputsDecoderRejectsInvalid(t *testing.T) {
	base := string(writerDetectorFixture(t, true))
	cases := map[string]string{
		"missing version":         strings.Replace(base, `"version":1,`, "", 1),
		"missing bool":            strings.Replace(base, `"same_document":true,`, "", 1),
		"null bool":               strings.Replace(base, `"same_document":true`, `"same_document":null`, 1),
		"unknown":                 strings.Replace(base, `"version":1`, `"version":1,"surprise":1`, 1),
		"duplicate":               strings.Replace(base, `"version":1`, `"version":1,"version":1`, 1),
		"bad digest":              strings.Replace(base, strings.Repeat("a", 64), strings.Repeat("z", 64), 1),
		"invalid state":           strings.Replace(base, `"state":"checked"`, `"state":"unknown"`, 1),
		"invalid location":        strings.Replace(base, `"start_line":6`, `"start_line":999999`, 1),
		"invalid page":            strings.Replace(base, `"page":1`, `"page":2`, 1),
		"missing main acceptance": strings.Replace(base, `,"main_accepted":false`, "", 1),
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			if value == base {
				t.Fatal("test mutation did not apply")
			}
			if _, err := fetch.DecodeDetectorInputsV1([]byte(value), "source_tree"); err == nil {
				t.Fatal("accepted invalid detector input")
			}
		})
	}
}

func TestDetectorInputsDecoderEnforcesSource(t *testing.T) {
	for _, tc := range []struct {
		full   bool
		source string
	}{{false, "ocr"}, {true, "html_transcription"}, {true, "none"}} {
		if _, err := fetch.DecodeDetectorInputsV1(writerDetectorFixture(t, tc.full), tc.source); err == nil {
			t.Fatalf("full=%v source=%s accepted", tc.full, tc.source)
		}
	}
}

func TestDetectorInputsDecoderRejectsImpossibleServedOCR(t *testing.T) {
	for _, source := range []string{"ocr", "none"} {
		var root map[string]any
		if err := json.Unmarshal(writerDetectorFixture(t, true), &root); err != nil {
			t.Fatal(err)
		}
		root["tree_transcription"] = map[string]any{"state": "no_pair"}
		ocr := root["ocr_check"].(map[string]any)
		if source == "ocr" {
			ocr["same_document"], ocr["verdict"] = false, "rejected"
			item := ocr["items"].([]any)[0].(map[string]any)
			item["position"] = "unlocated"
			delete(item, "location")
		} else {
			ocr["verdict"] = "clean"
			ocr["items"] = []any{}
		}
		encoded, err := json.Marshal(root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = fetch.DecodeDetectorInputsV1(encoded, source); err == nil {
			t.Fatalf("source=%s accepted impossible OCR serving", source)
		}
	}
}

func TestDetectorInputsDecoderRejectsNestedInvalid(t *testing.T) {
	base := string(writerDetectorFixture(t, true))
	for name, value := range map[string]string{
		"unknown item":           strings.Replace(base, `"kind":"article_label"`, `"kind":"article_label","extra":1`, 1),
		"duplicate item field":   strings.Replace(base, `"page":1`, `"page":1,"page":1`, 1),
		"payload on unavailable": strings.Replace(base, `"state":"checked"`, `"state":"unavailable"`, 1),
		"payload on no_pair":     strings.Replace(base, `"state":"measured"`, `"state":"no_pair"`, 1),
		"null items":             strings.Replace(base, `"items":[`, `"items":null,"ignored":[`, 1),
		"negative count":         strings.Replace(base, `"other_items":0`, `"other_items":-1`, 1),
		"location without tail":  strings.Replace(base, `"position":"tail"`, `"position":"not_tail"`, 1),
		"bad scope":              strings.Replace(base, `"tail_scope_index":1`, `"tail_scope_index":2`, 1),
		"bad ordinal":            strings.Replace(base, `"ordinal":1`, `"ordinal":2`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if value == base {
				t.Fatal("test mutation did not apply")
			}
			if _, err := fetch.DecodeDetectorInputsV1([]byte(value), "source_tree"); err == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(base), &root); err != nil {
		t.Fatal(err)
	}
	ocr := root["ocr_check"].(map[string]any)
	items := ocr["items"].([]any)
	ocr["items"] = append(items, items[0])
	encoded, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fetch.DecodeDetectorInputsV1(encoded, "source_tree"); err == nil {
		t.Fatal("accepted duplicate label tuple")
	}
}
