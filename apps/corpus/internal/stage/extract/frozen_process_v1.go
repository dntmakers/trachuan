package extract

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"danny.vn/trachuan/apps/corpus/internal/textparse"
	"danny.vn/trachuan/apps/corpus/internal/vbpl"
)

type frozenTextReader interface {
	ReadText(store.OcrText) ([]byte, error)
}

func verifyFrozenSelectionV1(ctx context.Context, dir, key, fetchHash, inputHash string, row store.GetRunOCRSelectionRow, ocr frozenTextReader) (selection, []vbpl.Node, []byte, error) {
	return verifyFrozenSelectionV1WithDecision(ctx, dir, key, fetchHash, inputHash, row, ocr, nil, nil)
}

func verifyFrozenSelectionV1WithDecision(ctx context.Context, dir, key, fetchHash, inputHash string, row store.GetRunOCRSelectionRow, ocr frozenTextReader, decision *selection, decisionNodes []vbpl.Node) (selection, []vbpl.Node, []byte, error) {
	selected, err := selectionFromRowV1(row, fetchHash)
	if err != nil {
		return selection{}, nil, nil, err
	}
	if observationInputHash(fetchHash, selected) != inputHash {
		return selection{}, nil, nil, fmt.Errorf("frozen OCR input hash mismatch")
	}
	var data []byte
	if selected.entry != nil {
		if decision != nil {
			name, found := decision.pdfNames[selected.hash]
			if !found {
				return selection{}, nil, nil, fmt.Errorf("frozen OCR PDF not found in bundle")
			}
			selected.name = name
		} else {
			doc, readErr := fetch.ReadDocument(dir, key)
			if readErr != nil {
				return selection{}, nil, nil, fmt.Errorf("read frozen OCR document: %w", readErr)
			}
			found := false
			for _, file := range doc.Files {
				if file.SHA256 == selected.hash && strings.EqualFold(filepath.Ext(file.Name), ".pdf") {
					selected.name = file.Name
					found = true
					break
				}
			}
			if !found {
				return selection{}, nil, nil, fmt.Errorf("frozen OCR PDF not found in bundle")
			}
		}
		check, checkErr := ocrstore.VerifyStoredCheckResult(row.CheckResult, selected.entry.Check.ResultSha256, selected.entry.Check.Flagged)
		if checkErr != nil {
			return selection{}, nil, nil, fmt.Errorf("verify frozen OCR check: %w", checkErr)
		}
		selected.verifiedCheck = &check
		if ocr == nil {
			return selection{}, nil, nil, fmt.Errorf("OCR store is not configured")
		}
		data, err = ocr.ReadText(selected.entry.Text)
		if err != nil {
			return selection{}, nil, nil, fmt.Errorf("read frozen OCR text: %w", err)
		}
		selected.entry.Check.Result = row.CheckResult
		selected.ocrParagraphs, selected.ocrParseErr = textparse.ParagraphsFromText(string(data))
		selected.ocrParsed = true
	}
	var materialized selection
	var nodes []vbpl.Node
	if decision != nil && selected.gap == "text_number_mismatch" {
		if decision.gap != selected.gap {
			return selection{}, nil, nil, fmt.Errorf("invalid frozen number mismatch")
		}
		materialized = selected
		materialized.reason = decision.reason
	} else if decision != nil && selected.gap == "tree_headings_only" {
		if decision.source != "source_tree" || decision.gap != selected.gap {
			return selection{}, nil, nil, fmt.Errorf("invalid frozen heading-only tree")
		}
		materialized, nodes = *decision, decisionNodes
		materialized.consulted = selected.consulted
	} else if selected.source == "ocr" {
		materialized = selected
	} else {
		materialized, nodes, err = materializeFrozen(ctx, dir, key, selected)
		if err != nil {
			return selection{}, nil, nil, err
		}
	}
	setTreeTranscriptionDefault(key, &materialized)
	materialized.hash, materialized.entry, materialized.ocrState = selected.hash, selected.entry, selected.ocrState
	materialized.verifiedCheck, materialized.ocrParagraphs, materialized.ocrParseErr, materialized.ocrParsed = selected.verifiedCheck, selected.ocrParagraphs, selected.ocrParseErr, selected.ocrParsed
	return materialized, nodes, data, nil
}
