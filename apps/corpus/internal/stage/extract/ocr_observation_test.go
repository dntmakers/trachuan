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
)

func observationSelection(t *testing.T, result ocrstore.CheckResult, text string) selection {
	t.Helper()
	dir, ocr, row, _, root := processV1Fixture(t)
	textHash := fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
	if err := os.WriteFile(filepath.Join(root, "text", textHash+".txt"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	row.TextSha256 = &textHash
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	row.CheckResult, row.ResultSha256, row.Flagged.Int32 = body, &digest, int32(len(result.Items))
	selected, err := selectionFromRowV1(row, *row.FetchSha256)
	if err != nil {
		t.Fatal(err)
	}
	selected, _, _, err = verifyFrozenSelectionV1(context.Background(), dir, "vn:vbpl:1", *row.FetchSha256, observationInputHash(*row.FetchSha256, selected), row, ocr)
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

func TestOCRObservationEmptyStates(t *testing.T) {
	for _, state := range []string{"not_selected", "unavailable"} {
		got, err := buildOCRCheckObservation(selection{ocrState: state}, nil)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(got)
		if err != nil || string(b) != `{"state":"`+state+`"}` {
			t.Fatalf("state=%s json=%s err=%v", state, b, err)
		}
	}
}

func TestOCRObservationCheckedItems(t *testing.T) {
	result := ocrstore.CheckResult{SameDocument: true, Pages: 10, PDFPages: 10, Verdict: "flagged", Items: []ocrstore.CheckItem{
		{Kind: "article_label", Page: 1, OCR: "Điều   5", PDF: "Điều 6", Note: "private note"},
		{Kind: "document_number", Page: 1, OCR: "42/2026", PDF: "43/2026"},
		{Kind: "article_label", Page: 2, OCR: "chung", PDF: "khác"},
		{Kind: "article_label", Page: 1, OCR: "MISSING", PDF: "Điều 7"},
		{Kind: "missing_text", Page: 3, OCR: "private text", PDF: "private pdf"},
	}}
	selected := observationSelection(t, result, locatorFixture())
	got, err := buildOCRCheckObservation(selected, []byte(locatorFixture()))
	if err != nil || got.State != "checked" || got.OCRCheckDetails == nil {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if !got.SameDocument || got.Verdict != "flagged" || !got.HasMissingText || got.PageCount != 10 || got.ParagraphCount != 8 || got.LineCount != 10 || got.ScopeCount != 2 || got.OtherItems != 1 || len(got.Items) != 4 {
		t.Fatalf("record=%+v", got.OCRCheckDetails)
	}
	wantKinds := []string{"article_label", "document_number", "article_label", "article_label"}
	wantOrdinals := []int{1, 1, 1, 2}
	wantPositions := []string{"tail", "unlocated", "ambiguous", "unlocated"}
	for i, item := range got.Items {
		if item.Kind != wantKinds[i] || item.Ordinal != wantOrdinals[i] || item.Position != wantPositions[i] || (item.Location != nil) != (i == 0) {
			t.Fatalf("item %d: %+v", i, item)
		}
	}
	if got.PDFSHA256 != selected.hash || got.EngineID != selected.entry.Text.EngineID || got.TextSHA256 != selected.entry.Text.TextSha256 || got.CheckerID != selected.entry.Check.CheckerID || got.ResultSHA256 != selected.entry.Check.ResultSha256 {
		t.Fatalf("identity=%+v", got.OCRCheckDetails)
	}
	if got.Items[0].Location == nil || got.Items[0].Location.TailScopeIndex != 1 || got.Items[0].Location.StartLine != 6 || got.Items[0].Location.EndLine != 6 {
		t.Fatalf("location=%+v", got.Items[0].Location)
	}
	if got.Items[0].OCRReadingSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte("Điều 5"))) || got.Items[0].PDFReadingSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte("Điều 6"))) {
		t.Fatalf("hashes=%+v", got.Items[0])
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"Điều 5", "Điều 6", "private note", "private text", "private pdf"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("record leaked %q: %s", secret, b)
		}
	}
}

func TestOCRObservationZeroItems(t *testing.T) {
	selected := observationSelection(t, ocrstore.CheckResult{SameDocument: true, Pages: 1, PDFPages: 1, Verdict: "clean", Items: []ocrstore.CheckItem{}}, locatorFixture())
	got, err := buildOCRCheckObservation(selected, []byte(locatorFixture()))
	b, marshalErr := json.Marshal(got)
	if err != nil || marshalErr != nil || !strings.Contains(string(b), `"items":[]`) {
		t.Fatalf("json=%s err=%v marshal=%v", b, err, marshalErr)
	}
}

func TestOCRObservationWrongDocumentAndParseLimit(t *testing.T) {
	for _, tc := range []struct {
		same       bool
		text       string
		paragraphs int
	}{
		{false, locatorFixture(), 8},
		{true, strings.Repeat("x\n", 50001), 0},
	} {
		verdict := "rejected"
		if tc.same {
			verdict = "flagged"
		}
		result := ocrstore.CheckResult{SameDocument: tc.same, Pages: 1, PDFPages: 1, Verdict: verdict, Items: []ocrstore.CheckItem{{Kind: "article_label", Page: 1, OCR: "Điều 5", PDF: "Điều 6"}}}
		got, err := buildOCRCheckObservation(observationSelection(t, result, tc.text), []byte(tc.text))
		if err != nil || got.OCRCheckDetails == nil || got.ParagraphCount != tc.paragraphs || len(got.Items) != 1 || got.Items[0].Position != "unlocated" || got.Items[0].Location != nil || (tc.paragraphs == 0 && got.ScopeCount != 0) {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	}
}

func TestOCRObservationFrozenHeadingState(t *testing.T) {
	digest, source := strings.Repeat("f", 64), "source_tree"
	for _, state := range []string{"not_selected", "unavailable"} {
		row := store.GetRunOCRSelectionRow{SelectionVersion: 1, FetchSha256: &digest, SelectionState: &state, TextSource: &source, Gap: "tree_headings_only"}
		selected, err := selectionFromRowV1(row, digest)
		if err != nil {
			t.Fatal(err)
		}
		dir := headingBundle(t, bareTree, false)
		selected, _, _, err = verifyFrozenSelectionV1(context.Background(), dir, "vn:vbpl:1", digest, observationInputHash(digest, selected), row, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := buildOCRCheckObservation(selected, nil)
		if err != nil || got.State != state {
			t.Fatalf("state=%s got=%+v err=%v", state, got, err)
		}
	}
}
