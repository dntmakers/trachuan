package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

func writeLineageText(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	if data != nil {
		if err := os.WriteFile(filepath.Join(dir, "text.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestContractLineageMissingAndMismatch(t *testing.T) {
	fetchHash := strings.Repeat("f", 64)
	dir := writeLineageText(t, nil)
	if err := fetch.CheckExtractLineageContract(dir, fetchHash, fetchHash, "legacy"); err != nil {
		t.Fatalf("legacy missing: %v", err)
	}
	if err := fetch.CheckExtractLineageContract(dir, fetchHash, fetchHash, "detector-inputs/1"); err == nil {
		t.Fatal("v1 accepted missing record")
	}
	if err := fetch.CheckExtractLineageContract(dir, fetchHash, fetchHash, "unknown"); err == nil {
		t.Fatal("accepted unknown contract")
	}
	selected := selection{source: "source_tree", ocrState: "not_selected", treeTranscription: treeTranscriptionObservation{State: "no_pair"}}
	data, err := marshalV1TextRecord(nil, selected, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir = writeLineageText(t, data)
	if err = fetch.CheckExtractLineageContract(dir, fetchHash, fetchHash, "detector-inputs/1"); err != nil {
		t.Fatalf("v1 metadata: %v", err)
	}
	if err = fetch.CheckExtractLineageContract(dir, fetchHash, fetchHash, "legacy"); err == nil {
		t.Fatal("legacy accepted v1 record")
	}
	dir = writeLineageText(t, []byte(`{"text_source":"source_tree"}`))
	if err = fetch.CheckExtractLineageContract(dir, fetchHash, fetchHash, "detector-inputs/1"); err == nil {
		t.Fatal("v1 accepted legacy record")
	}
}

func TestContractLineageCheckedRoundTripAndMutation(t *testing.T) {
	for _, served := range []bool{false, true} {
		check := ocrstore.CheckResult{SameDocument: false, Pages: 1, PDFPages: 1, Verdict: "rejected", Items: []ocrstore.CheckItem{}}
		if served {
			check = ocrstore.CheckResult{SameDocument: true, Pages: 1, PDFPages: 1, Verdict: "clean", Items: []ocrstore.CheckItem{}}
		}
		selected := observationSelection(t, check, locatorFixture())
		selected.treeTranscription = treeTranscriptionObservation{State: "no_pair"}
		record := &textRecord{TextSource: "none", Gap: "ocr_rejected"}
		if served {
			selected.source = "ocr"
			selected.gap = ""
			record = &textRecord{TextSource: "ocr", SHA256: selected.hash, EngineID: selected.entry.Text.EngineID, TextSHA256: selected.entry.Text.TextSha256, CheckerID: selected.entry.Check.CheckerID, ResultSHA256: selected.entry.Check.ResultSha256}
		}
		encoded, err := marshalV1TextRecord(record, selected, []byte(locatorFixture()))
		if err != nil {
			t.Fatal(err)
		}
		base := selectionInputHash("fetch", selected)
		want := stage.RecipeHash("extract-ocr-observation", "1", base, selected.hash, selected.entry.Text.EngineID, selected.entry.Text.TextSha256, selected.entry.Check.CheckerID, selected.entry.Check.ResultSha256)
		if got := observationInputHash("fetch", selected); got != want {
			t.Fatalf("got=%s want=%s", got, want)
		}
		dir := writeLineageText(t, encoded)
		if err = fetch.CheckExtractLineageContract(dir, "fetch", want, "detector-inputs/1"); err != nil {
			t.Fatalf("served=%v err=%v json=%s", served, err, encoded)
		}
		var obj map[string]any
		if err = json.Unmarshal(encoded, &obj); err != nil {
			t.Fatal(err)
		}
		if served {
			obj["engine_id"] = "changed"
		} else {
			obj["detector_inputs"].(map[string]any)["ocr_check"].(map[string]any)["pdf_sha256"] = strings.Repeat("d", 64)
		}
		changed, err := json.Marshal(obj)
		if err != nil {
			t.Fatal(err)
		}
		if err = fetch.CheckExtractLineageContract(writeLineageText(t, changed), "fetch", want, "detector-inputs/1"); err == nil {
			t.Fatalf("served=%v accepted mutation", served)
		}
	}
}

func TestContractLineageUnchangedLegacyHash(t *testing.T) {
	text, result := strings.Repeat("b", 64), strings.Repeat("c", 64)
	data := []byte(`{"text_source":"ocr","text_sha256":"` + text + `","result_sha256":"` + result + `"}`)
	dir := writeLineageText(t, data)
	hash := fetch.ExtractInputHash("fetch", "ocr", text, result)
	if err := fetch.CheckExtractLineage(dir, "fetch", hash); err != nil {
		t.Fatal(err)
	}
	if err := fetch.CheckExtractLineageContract(dir, "fetch", hash, "legacy"); err != nil {
		t.Fatal(err)
	}
	if err := fetch.CheckExtractLineageContract(dir, "fetch", hash, "detector-inputs/1"); err == nil {
		t.Fatal("accepted legacy record as v1")
	}
}

func TestContractLineageServedAllBindings(t *testing.T) {
	check := ocrstore.CheckResult{SameDocument: true, Pages: 1, PDFPages: 1, Verdict: "clean", Items: []ocrstore.CheckItem{}}
	selected := observationSelection(t, check, locatorFixture())
	selected.source, selected.gap = "ocr", ""
	selected.treeTranscription = treeTranscriptionObservation{State: "no_pair"}
	record := &textRecord{TextSource: "ocr", SHA256: selected.hash, EngineID: selected.entry.Text.EngineID, TextSHA256: selected.entry.Text.TextSha256, CheckerID: selected.entry.Check.CheckerID, ResultSHA256: selected.entry.Check.ResultSha256}
	data, err := marshalV1TextRecord(record, selected, []byte(locatorFixture()))
	if err != nil {
		t.Fatal(err)
	}
	hash := observationInputHash("fetch", selected)
	for _, key := range []string{"sha256", "engine_id", "text_sha256", "checker_id", "result_sha256"} {
		var recordJSON map[string]any
		if err = json.Unmarshal(data, &recordJSON); err != nil {
			t.Fatal(err)
		}
		recordJSON[key] = "changed"
		changed, err := json.Marshal(recordJSON)
		if err != nil {
			t.Fatal(err)
		}
		if err = fetch.CheckExtractLineageContract(writeLineageText(t, changed), "fetch", hash, "detector-inputs/1"); err == nil {
			t.Fatalf("accepted changed %s", key)
		}
	}
}

func TestContractLineageCheckedTree(t *testing.T) {
	check := ocrstore.CheckResult{SameDocument: false, Pages: 1, PDFPages: 1, Verdict: "rejected", Items: []ocrstore.CheckItem{}}
	selected := observationSelection(t, check, locatorFixture())
	selected.source = "source_tree"
	selected.treeTranscription = treeTranscriptionObservation{State: "no_pair"}
	data, err := marshalV1TextRecord(nil, selected, []byte(locatorFixture()))
	if err != nil {
		t.Fatal(err)
	}
	dir := writeLineageText(t, data)
	if err = fetch.CheckExtractLineageContract(dir, "fetch", observationInputHash("fetch", selected), "detector-inputs/1"); err != nil {
		t.Fatal(err)
	}
	if err = fetch.CheckExtractLineageContract(dir, "fetch", "fetch", "detector-inputs/1"); err == nil {
		t.Fatal("accepted unbound checked tree")
	}
}
