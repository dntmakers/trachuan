package extract

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func processV1Fixture(t *testing.T) (string, *ocrstore.Store, store.GetRunOCRSelectionRow, string, string) {
	t.Helper()
	pdfBytes := []byte("%PDF-1.4 synthetic fixture\n")
	pdf := fmt.Sprintf("%x", sha256.Sum256(pdfBytes))
	digest, state, source := strings.Repeat("f", 64), "checked", "none"
	text := []byte("OCR text\n")
	textHash := fmt.Sprintf("%x", sha256.Sum256(text))
	result, err := json.Marshal(ocrstore.CheckResult{SameDocument: false, Pages: 1, PDFPages: 1, Items: []ocrstore.CheckItem{}, Verdict: "rejected"})
	if err != nil {
		t.Fatal(err)
	}
	resultHash := fmt.Sprintf("%x", sha256.Sum256(result))
	textID, checkID := int64(1), int64(2)
	engine, checker := "engine", "checker"
	row := store.GetRunOCRSelectionRow{SelectionVersion: 1, SelectionState: &state, TextSource: &source, FetchSha256: &digest, Gap: "ocr_rejected", PdfSha256: &pdf, OcrTextID: &textID, OcrCheckID: &checkID, TextPdfSha256: &pdf, CheckOcrTextID: &textID, EngineID: &engine, TextSha256: &textHash, CheckerID: &checker, ResultSha256: &resultHash, Flagged: pgtype.Int4{Valid: true}, CheckResult: result}
	dir, root := t.TempDir(), t.TempDir()
	for name, data := range map[string]string{"detail.json": `{"data":{"id":"1"}}`, "files.json": fmt.Sprintf(`[{"name":"scan.pdf","sha256":"%s","size":%d}]`, pdf, len(pdfBytes))} {
		if writeErr := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if mkdirErr := os.Mkdir(filepath.Join(dir, "files"), 0700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(dir, "files", pdf), pdfBytes, 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if mkdirErr := os.Mkdir(filepath.Join(root, "text"), 0700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(root, "text", textHash+".txt"), text, 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	selected, err := selectionFromRowV1(row, digest)
	if err != nil {
		t.Fatal(err)
	}
	return dir, ocrstore.Open(root, nil), row, observationInputHash(digest, selected), root
}

func TestVerifyFrozenV1Withheld(t *testing.T) {
	dir, ocr, row, hash, root := processV1Fixture(t)
	_ = root
	selected, _, data, err := verifyFrozenSelectionV1(context.Background(), dir, "vn:vbpl:1", *row.FetchSha256, hash, row, ocr)
	if err != nil || selected.entry == nil || selected.entry.Check == nil || selected.gap != "ocr_rejected" || string(data) != "OCR text\n" {
		t.Fatalf("selected=%+v data=%q err=%v", selected, data, err)
	}
}

func TestVerifyFrozenV1RejectsChangedInputs(t *testing.T) {
	for _, change := range []string{"input", "pdf", "result", "text"} {
		t.Run(change, func(t *testing.T) {
			dir, ocr, row, hash, root := processV1Fixture(t)
			switch change {
			case "input":
				hash = strings.Repeat("0", 64)
			case "pdf":
				if err := os.WriteFile(filepath.Join(dir, "files.json"), []byte(`[{"name":"scan.pdf","sha256":"`+strings.Repeat("b", 64)+`","size":1}]`), 0600); err != nil {
					t.Fatal(err)
				}
			case "result":
				row.CheckResult = []byte(`{"same_document":true,"pages":1,"pdf_pages":1,"items":[],"verdict":"clean","dropped_equal":0}`)
			case "text":
				if err := os.WriteFile(filepath.Join(root, "text", *row.TextSha256+".txt"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, _, err := verifyFrozenSelectionV1(context.Background(), dir, "vn:vbpl:1", *row.FetchSha256, hash, row, ocr); err == nil {
				t.Fatal("accepted changed " + change)
			}
		})
	}
}

func TestVerifyFrozenV1HeadingFallbackRetainsCheck(t *testing.T) {
	dir, ocr, row, _, _ := processV1Fixture(t)
	if err := os.WriteFile(filepath.Join(dir, "tree.json"), []byte(bareTree), 0600); err != nil {
		t.Fatal(err)
	}
	source := "source_tree"
	row.TextSource = &source
	row.Gap = "tree_headings_only"
	selected, err := selectionFromRowV1(row, *row.FetchSha256)
	if err != nil {
		t.Fatal(err)
	}
	hash := observationInputHash(*row.FetchSha256, selected)
	got, nodes, data, err := verifyFrozenSelectionV1(context.Background(), dir, "vn:vbpl:1", *row.FetchSha256, hash, row, ocr)
	if err != nil || len(nodes) == 0 || got.entry == nil || got.entry.Check == nil || got.hash != *row.PdfSha256 || string(data) != "OCR text\n" {
		t.Fatalf("got=%+v nodes=%d data=%q err=%v", got, len(nodes), data, err)
	}
}
