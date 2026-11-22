package extract

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"danny.vn/trachuan/apps/corpus/internal/vbpl"
)

type frozenTextReader interface {
	ReadText(store.OcrText) ([]byte, error)
}

func verifyFrozenSelectionV1(ctx context.Context, dir, key, fetchHash, inputHash string, row store.GetRunOCRSelectionRow, ocr frozenTextReader) (selection, []vbpl.Node, []byte, error) {
	selected, err := selectionFromRowV1(row, fetchHash)
	if err != nil {
		return selection{}, nil, nil, err
	}
	if observationInputHash(fetchHash, selected) != inputHash {
		return selection{}, nil, nil, fmt.Errorf("frozen OCR input hash mismatch")
	}
	var data []byte
	if selected.entry != nil {
		doc, readErr := fetch.ReadDocument(dir, key)
		if readErr != nil {
			return selection{}, nil, nil, fmt.Errorf("read frozen OCR document: %w", readErr)
		}
		found := false
		for _, file := range doc.Files {
			if file.SHA256 == selected.hash && strings.EqualFold(filepath.Ext(file.Name), ".pdf") {
				found = true
				break
			}
		}
		if !found {
			return selection{}, nil, nil, fmt.Errorf("frozen OCR PDF not found in bundle")
		}
		if err = ocrstore.VerifyStoredCheck(row.CheckResult, selected.entry.Check.ResultSha256, selected.entry.Check.Flagged); err != nil {
			return selection{}, nil, nil, fmt.Errorf("verify frozen OCR check: %w", err)
		}
		if ocr == nil {
			return selection{}, nil, nil, fmt.Errorf("OCR store is not configured")
		}
		data, err = ocr.ReadText(selected.entry.Text)
		if err != nil {
			return selection{}, nil, nil, fmt.Errorf("read frozen OCR text: %w", err)
		}
		selected.entry.Check.Result = row.CheckResult
	}
	materialized, nodes, err := materializeFrozen(ctx, dir, key, selected)
	if err != nil {
		return selection{}, nil, nil, err
	}
	materialized.hash, materialized.entry, materialized.ocrState = selected.hash, selected.entry, selected.ocrState
	return materialized, nodes, data, nil
}
