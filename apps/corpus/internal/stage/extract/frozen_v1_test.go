package extract

import (
	"context"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type v1SelectionDB struct {
	row      *store.GetRunOCRSelectionRow
	conflict *store.GetRunOCRSelectionRow
	entry    ocrstore.Entry
}

func (f *v1SelectionDB) GetRunOCRSelection(context.Context, store.GetRunOCRSelectionParams) (store.GetRunOCRSelectionRow, error) {
	if f.row == nil {
		return store.GetRunOCRSelectionRow{}, pgx.ErrNoRows
	}
	return *f.row, nil
}
func (f *v1SelectionDB) InsertRunOCRSelection(_ context.Context, p store.InsertRunOCRSelectionParams) error {
	if f.conflict != nil {
		f.row = f.conflict
		return nil
	}
	r := store.GetRunOCRSelectionRow{RunID: p.RunID, ItemKey: p.ItemKey, Gap: p.Gap, PdfSha256: p.PdfSha256, OcrTextID: p.OcrTextID, OcrCheckID: p.OcrCheckID, SelectionVersion: p.SelectionVersion, SelectionState: p.SelectionState, TextSource: p.TextSource, FetchSha256: p.FetchSha256}
	if p.OcrTextID != nil {
		r.TextPdfSha256 = &f.entry.Text.PdfSha256
		r.CheckOcrTextID = &f.entry.Check.OcrTextID
		r.EngineID = &f.entry.Text.EngineID
		r.TextSha256 = &f.entry.Text.TextSha256
		r.CheckerID = &f.entry.Check.CheckerID
		r.ResultSha256 = &f.entry.Check.ResultSha256
		r.Flagged = pgtype.Int4{Int32: f.entry.Check.Flagged, Valid: true}
	}
	f.row = &r
	return nil
}

func TestPlanFrozenV1CheckedWithheldRoundTrip(t *testing.T) {
	pdf, digest := strings.Repeat("a", 64), strings.Repeat("f", 64)
	entry := ocrstore.Entry{Text: store.OcrText{ID: 1, PdfSha256: pdf, EngineID: "engine", TextSha256: strings.Repeat("b", 64)}, Check: &store.OcrCheck{ID: 2, OcrTextID: 1, CheckerID: "checker", ResultSha256: strings.Repeat("c", 64)}}
	db := &v1SelectionDB{entry: entry}
	choose := func() (selection, error) {
		return selection{source: "none", gap: "ocr_rejected", hash: pdf, entry: &entry, consulted: true}, nil
	}
	got, hash, err := planFrozenSelectionV1(context.Background(), db, 1, "key", digest, choose)
	if err != nil || got.entry == nil || got.entry.Check == nil || got.entry.Text.ID != 1 || got.entry.Check.ID != 2 || got.gap != "ocr_rejected" || hash != observationInputHash(digest, got) {
		t.Fatalf("got=%+v hash=%s err=%v", got, hash, err)
	}
	if db.row.SelectionState == nil || *db.row.SelectionState != "checked" {
		t.Fatalf("row=%+v", db.row)
	}
	_, replayHash, err := planFrozenSelectionV1(context.Background(), db, 1, "key", digest, func() (selection, error) { t.Fatal("replay called choose"); return selection{}, nil })
	if err != nil || replayHash != hash {
		t.Fatalf("hash=%s err=%v", replayHash, err)
	}
}

func TestPlanFrozenV1RejectsChangedFetchOnReplay(t *testing.T) {
	old, changed, state, source := strings.Repeat("a", 64), strings.Repeat("b", 64), "unavailable", "none"
	row := store.GetRunOCRSelectionRow{SelectionVersion: 1, SelectionState: &state, TextSource: &source, FetchSha256: &old, Gap: "needs_ocr"}
	for _, conflict := range []bool{false, true} {
		db := &v1SelectionDB{}
		if conflict {
			db.conflict = &row
		} else {
			db.row = &row
		}
		_, _, err := planFrozenSelectionV1(context.Background(), db, 1, "key", changed, func() (selection, error) { return selection{source: "none", gap: "needs_ocr", consulted: true}, nil })
		if err == nil {
			t.Fatalf("conflict=%v accepted changed fetch", conflict)
		}
	}
}

func TestPlanFrozenV1RejectsBrokenLinks(t *testing.T) {
	pdf, digest, state, source := strings.Repeat("a", 64), strings.Repeat("b", 64), "checked", "none"
	other := strings.Repeat("c", 64)
	textID, checkID, linkedID := int64(1), int64(2), int64(3)
	engine, text, checker, result := "engine", strings.Repeat("d", 64), "checker", strings.Repeat("e", 64)
	row := store.GetRunOCRSelectionRow{SelectionVersion: 1, SelectionState: &state, TextSource: &source, FetchSha256: &digest, Gap: "ocr_rejected", PdfSha256: &pdf, OcrTextID: &textID, OcrCheckID: &checkID, TextPdfSha256: &other, CheckOcrTextID: &linkedID, EngineID: &engine, TextSha256: &text, CheckerID: &checker, ResultSha256: &result, Flagged: pgtype.Int4{Valid: true}}
	for _, fixPDF := range []bool{false, true} {
		if fixPDF {
			row.TextPdfSha256 = &pdf
		}
		db := &v1SelectionDB{row: &row}
		_, _, err := planFrozenSelectionV1(context.Background(), db, 1, "key", digest, nil)
		if err == nil {
			t.Fatalf("fixPDF=%v accepted broken link", fixPDF)
		}
	}
}

func TestPlanFrozenV1RejectsLegacyAndInvalidMatrix(t *testing.T) {
	digest, state, source := strings.Repeat("a", 64), "unavailable", "none"
	row := store.GetRunOCRSelectionRow{SelectionVersion: 1, SelectionState: &state, TextSource: &source, FetchSha256: &digest, Gap: "needs_ocr"}
	for _, mutate := range []func(*store.GetRunOCRSelectionRow){
		func(r *store.GetRunOCRSelectionRow) { r.SelectionVersion = 0 },
		func(r *store.GetRunOCRSelectionRow) { r.Gap = "ocr_rejected" },
		func(r *store.GetRunOCRSelectionRow) { id := int64(1); r.OcrTextID = &id },
	} {
		bad := row
		mutate(&bad)
		if _, _, err := planFrozenSelectionV1(context.Background(), &v1SelectionDB{row: &bad}, 1, "key", digest, nil); err == nil {
			t.Fatalf("accepted row %+v", bad)
		}
	}
}

func TestPlanFrozenV1UnavailableAndNotSelected(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, tc := range []struct {
		selected selection
		state    string
	}{
		{selection{source: "none", gap: "needs_ocr", consulted: true}, "unavailable"},
		{selection{source: "none", gap: "text_number_mismatch"}, "not_selected"},
		{selection{source: "source_tree", gap: "tree_headings_only"}, "not_selected"},
	} {
		db := &v1SelectionDB{}
		got, hash, err := planFrozenSelectionV1(context.Background(), db, 1, "key", digest, func() (selection, error) { return tc.selected, nil })
		if err != nil || db.row == nil || *db.row.SelectionState != tc.state || got.source != tc.selected.source || got.gap != tc.selected.gap || hash != selectionInputHash(digest, got) {
			t.Fatalf("selected=%+v got=%+v hash=%s err=%v", tc.selected, got, hash, err)
		}
	}
}
