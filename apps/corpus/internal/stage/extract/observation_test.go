package extract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
	"danny.vn/trachuan/apps/corpus/internal/store"
)

func TestObservationInputHash(t *testing.T) {
	entry := &ocrstore.Entry{Text: store.OcrText{PdfSha256: "pdf", EngineID: "engine", TextSha256: "text"}, Check: &store.OcrCheck{CheckerID: "checker", ResultSha256: "result"}}
	for _, source := range []string{"ocr", "none", "source_tree"} {
		s := selection{source: source, gap: "ocr_rejected", hash: "pdf", entry: entry}
		base := selectionInputHash("fetch", s)
		want := stage.RecipeHash("extract-ocr-observation", "1", base, "pdf", "engine", "text", "checker", "result")
		if got := observationInputHash("fetch", s); got != want {
			t.Errorf("source %s: got %s want %s", source, got, want)
		}
	}
	s := selection{source: "none", gap: "needs_ocr"}
	if got, want := observationInputHash("fetch", s), selectionInputHash("fetch", s); got != want {
		t.Errorf("without check: got %s want %s", got, want)
	}
}

func TestWithheldSelectionRetainsCompletedCheck(t *testing.T) {
	src := ocrFixture(t, true, 0)
	hash := strings.Repeat("a", 64)
	entry := src.entries[hash]
	result := ocrstore.CheckResult{SameDocument: false, Pages: 10, PDFPages: 10, Items: []ocrstore.CheckItem{}, Verdict: "rejected"}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	entry.Check.Result = data
	src.entries[hash] = entry
	selected, err := selectOCRFiles(context.Background(), t.TempDir(), "1", []fetch.DocumentFile{{Name: "scan.pdf", SHA256: hash}}, src, []string{"vision-ocr/builtin-latest"}, "checker")
	if err != nil || selected.gap != "ocr_rejected" || selected.hash != hash || selected.entry == nil || selected.entry.Check == nil {
		t.Fatalf("selection=%+v err=%v", selected, err)
	}
}

func TestHeadingFallbackRetainsCompletedCheck(t *testing.T) {
	dir := headingBundle(t, bareTree, false)
	hash := strings.Repeat("a", 64)
	if err := os.WriteFile(filepath.Join(dir, "files.json"), []byte(`[{"name":"scan.pdf","sha256":"`+hash+`","size":1}]`), 0600); err != nil {
		t.Fatal(err)
	}
	src := ocrFixture(t, true, 0)
	src.data = []byte("short")
	selected, _, err := chooseWithOCR(context.Background(), dir, "vn:vbpl:1", src, []string{"vision-ocr/builtin-latest"}, "checker")
	if err != nil || selected.source != "source_tree" || selected.hash != hash || selected.entry == nil || selected.entry.Check == nil {
		t.Fatalf("selection=%+v err=%v", selected, err)
	}
}
