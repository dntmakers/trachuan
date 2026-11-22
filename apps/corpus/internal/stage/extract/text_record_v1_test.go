package extract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
)

func TestV1TextRecordMetadataOnly(t *testing.T) {
	selected := selection{source: "source_tree", ocrState: "not_selected", treeTranscription: treeTranscriptionObservation{State: "no_pair"}}
	data, err := marshalV1TextRecord(nil, selected, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err = json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || string(got["text_source"]) != `"source_tree"` {
		t.Fatalf("record=%s", data)
	}
	var detector struct {
		Version int            `json:"version"`
		OCR     map[string]any `json:"ocr_check"`
		Tree    map[string]any `json:"tree_transcription"`
	}
	if err = json.Unmarshal(got["detector_inputs"], &detector); err != nil || detector.Version != 1 || detector.OCR["state"] != "not_selected" || detector.Tree["state"] != "no_pair" {
		t.Fatalf("detector=%+v err=%v", detector, err)
	}
}

func TestV1TextRecordPreservesLegacyRecord(t *testing.T) {
	record := &textRecord{TextSource: "html_transcription", FileName: "1_content.html", SHA256: strings.Repeat("a", 64)}
	legacy, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	selected := selection{source: "html_transcription", treeTranscription: treeTranscriptionObservation{State: "measured", TreeChars: 3, TranscriptionChars: 5, TreeTextSHA256: strings.Repeat("a", 64), TranscriptionTextSHA256: strings.Repeat("b", 64), TranscriptionFileSHA256: strings.Repeat("c", 64), MainAccepted: false}}
	encoded, err := marshalV1TextRecord(record, selected, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(record)
	if err != nil || string(after) != string(legacy) {
		t.Fatalf("legacy changed: %s / %s err=%v", legacy, after, err)
	}
	var original, updated map[string]json.RawMessage
	if err = json.Unmarshal(legacy, &original); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(encoded, &updated); err != nil {
		t.Fatal(err)
	}
	var detector struct {
		Version int            `json:"version"`
		Tree    map[string]any `json:"tree_transcription"`
		OCR     map[string]any `json:"ocr_check"`
	}
	if err = json.Unmarshal(updated["detector_inputs"], &detector); err != nil {
		t.Fatal(err)
	}
	delete(updated, "detector_inputs")
	if !reflect.DeepEqual(original, updated) || detector.Version != 1 || detector.Tree["state"] != "measured" || detector.Tree["main_accepted"] != false || detector.Tree["transcription_file_sha256"] != strings.Repeat("c", 64) || detector.OCR["state"] != "not_selected" {
		t.Fatalf("legacy=%s v1=%s", legacy, encoded)
	}
}

func TestV1TextRecordRejectsIncompleteInput(t *testing.T) {
	for _, selected := range []selection{
		{source: "none", treeTranscription: treeTranscriptionObservation{State: "no_pair"}},
		{source: "source_tree", treeTranscription: treeTranscriptionObservation{State: "unknown"}},
		{source: "source_tree", treeTranscription: treeTranscriptionObservation{State: "measured"}},
	} {
		if data, err := marshalV1TextRecord(nil, selected, nil); err == nil {
			t.Fatalf("accepted %+v: %s", selected, data)
		}
	}
}

func TestV1HeadingFallbackCandidateState(t *testing.T) {
	for _, candidate := range []bool{false, true} {
		dir := headingBundle(t, bareTree, false)
		if candidate {
			files := `[{"name":"scan.pdf","sha256":"` + strings.Repeat("a", 64) + `","size":1}]`
			if err := os.WriteFile(filepath.Join(dir, "files.json"), []byte(files), 0600); err != nil {
				t.Fatal(err)
			}
		}
		choose := func() (selection, error) {
			s, _, err := chooseWithOCR(context.Background(), dir, "vn:vbpl:1", nil, nil, "")
			return s, err
		}
		legacy, err := choose()
		if err != nil || !legacy.consulted || legacy.source != "source_tree" || legacy.gap != "tree_headings_only" {
			t.Fatalf("legacy=%+v err=%v", legacy, err)
		}
		q := &v1SelectionDB{}
		got, _, err := planFrozenSelectionV1(context.Background(), q, 1, "vn:vbpl:1", strings.Repeat("f", 64), choose)
		want := "not_selected"
		if candidate {
			want = "unavailable"
		}
		if err != nil || got.ocrState != want || q.row.SelectionState == nil || *q.row.SelectionState != want {
			t.Fatalf("candidate=%v selected=%+v row=%+v err=%v", candidate, got, q.row, err)
		}
	}
}

func TestV1TextRecordIncludesCheckedWithheld(t *testing.T) {
	result := ocrstore.CheckResult{SameDocument: false, Pages: 1, PDFPages: 1, Verdict: "rejected", Items: []ocrstore.CheckItem{{Kind: "article_label", Page: 1, OCR: "Điều 5", PDF: "Điều 6"}}}
	selected := observationSelection(t, result, locatorFixture())
	selected.treeTranscription = treeTranscriptionObservation{State: "no_pair"}
	record := &textRecord{TextSource: "none", Gap: "ocr_rejected"}
	encoded, err := marshalV1TextRecord(record, selected, []byte(locatorFixture()))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Detector struct {
			OCR struct {
				State string `json:"state"`
				Items []struct {
					Position string `json:"position"`
				} `json:"items"`
			} `json:"ocr_check"`
		} `json:"detector_inputs"`
	}
	if err = json.Unmarshal(encoded, &got); err != nil || got.Detector.OCR.State != "checked" || len(got.Detector.OCR.Items) != 1 || got.Detector.OCR.Items[0].Position != "unlocated" || strings.Contains(string(encoded), "Điều 5") {
		t.Fatalf("record=%s err=%v", encoded, err)
	}
}
