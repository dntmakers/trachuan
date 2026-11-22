package extract

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"github.com/jackc/pgx/v5"
)

func planFrozenSelectionV1(ctx context.Context, q selectionQueries, runID int64, key, fetchHash string, choose func() (selection, error)) (selection, string, error) {
	if !validSelectionHash(fetchHash) {
		return selection{}, "", fmt.Errorf("invalid fetch digest")
	}
	params := store.GetRunOCRSelectionParams{RunID: runID, ItemKey: key}
	row, err := q.GetRunOCRSelection(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		selected, selectErr := choose()
		if selectErr != nil {
			return selection{}, "", selectErr
		}
		if !selected.consulted && selected.gap != "text_number_mismatch" && (selected.source != "source_tree" || selected.gap != "tree_headings_only") {
			return selected, observationInputHash(fetchHash, selected), nil
		}
		state := selected.ocrState
		if state == "" {
			state = "not_selected"
			if selected.entry != nil && selected.entry.Check != nil {
				state = "checked"
			} else if selected.consulted {
				state = "unavailable"
			}
		}
		if selected.gap == "text_number_mismatch" {
			state = "not_selected"
		}
		insert := store.InsertRunOCRSelectionParams{RunID: runID, ItemKey: key, Gap: selected.gap, SelectionVersion: 1, SelectionState: &state, TextSource: &selected.source, FetchSha256: &fetchHash}
		if state == "checked" {
			if selected.entry == nil || selected.entry.Check == nil {
				return selection{}, "", fmt.Errorf("missing selected OCR check")
			}
			insert.PdfSha256, insert.OcrTextID, insert.OcrCheckID = &selected.hash, &selected.entry.Text.ID, &selected.entry.Check.ID
		} else if selected.entry != nil && selected.entry.Check != nil {
			return selection{}, "", fmt.Errorf("unexpected selected OCR check")
		}
		if err = q.InsertRunOCRSelection(ctx, insert); err != nil {
			return selection{}, "", fmt.Errorf("record OCR selection: %w", err)
		}
		row, err = q.GetRunOCRSelection(ctx, params)
	}
	if err != nil {
		return selection{}, "", fmt.Errorf("read OCR selection: %w", err)
	}
	selected, err := selectionFromRowV1(row, fetchHash)
	if err != nil {
		return selection{}, "", err
	}
	return selected, observationInputHash(fetchHash, selected), nil
}

func validSelectionHash(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func selectionFromRowV1(row store.GetRunOCRSelectionRow, fetchHash string) (selection, error) {
	if row.SelectionVersion != 1 || row.FetchSha256 == nil || *row.FetchSha256 != fetchHash || row.SelectionState == nil || row.TextSource == nil {
		return selection{}, fmt.Errorf("invalid frozen OCR version or fetch digest")
	}
	state, source := *row.SelectionState, *row.TextSource
	valid := false
	switch state {
	case "checked":
		valid = (source == "ocr" && row.Gap == "") || (source == "none" && (row.Gap == "ocr_rejected" || row.Gap == "ocr_label_flagged")) || (source == "source_tree" && row.Gap == "tree_headings_only")
	case "unavailable":
		valid = (source == "none" && (row.Gap == "needs_ocr" || row.Gap == "text_source_ambiguous")) || (source == "source_tree" && row.Gap == "tree_headings_only")
	case "not_selected":
		valid = (source == "none" && row.Gap == "text_number_mismatch") || (source == "source_tree" && row.Gap == "tree_headings_only")
	}
	if !valid {
		return selection{}, fmt.Errorf("invalid frozen OCR state")
	}
	if state != "checked" {
		if row.PdfSha256 != nil || row.OcrTextID != nil || row.OcrCheckID != nil {
			return selection{}, fmt.Errorf("unexpected frozen OCR identity")
		}
		selected, err := selectionFromRow(row)
		selected.ocrState = state
		return selected, err
	}
	if row.PdfSha256 == nil || row.OcrTextID == nil || row.OcrCheckID == nil || row.TextPdfSha256 == nil || row.CheckOcrTextID == nil || row.EngineID == nil || row.TextSha256 == nil || row.CheckerID == nil || row.ResultSha256 == nil || !row.Flagged.Valid || row.Flagged.Int32 < 0 || *row.OcrTextID <= 0 || *row.OcrCheckID <= 0 || *row.TextPdfSha256 != *row.PdfSha256 || *row.CheckOcrTextID != *row.OcrTextID || !validSelectionHash(*row.PdfSha256) || !validSelectionHash(*row.TextSha256) || !validSelectionHash(*row.ResultSha256) || *row.EngineID == "" || *row.CheckerID == "" {
		return selection{}, fmt.Errorf("invalid frozen OCR identity")
	}
	selected, err := selectionFromRow(row)
	if err != nil {
		return selection{}, err
	}
	selected.hash = *row.PdfSha256
	selected.ocrState = state
	selected.entry = &ocrstore.Entry{Text: store.OcrText{ID: *row.OcrTextID, PdfSha256: *row.TextPdfSha256, EngineID: *row.EngineID, TextSha256: *row.TextSha256}, Check: &store.OcrCheck{ID: *row.OcrCheckID, OcrTextID: *row.CheckOcrTextID, CheckerID: *row.CheckerID, ResultSha256: *row.ResultSha256, Flagged: row.Flagged.Int32}}
	return selected, nil
}
