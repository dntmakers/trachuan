package extract

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"danny.vn/trachuan/apps/corpus/internal/textparse"
	"danny.vn/trachuan/apps/corpus/internal/vbpl"
	"github.com/jackc/pgx/v5"
)

func verifiedFetchMembers(path, expected string) (map[string]string, error) {
	sum, members, err := artifact.VerifyWithMembers(path)
	if err != nil {
		return nil, stage.Retryable(fmt.Errorf("verify fetch artifact: %w", err))
	}
	if sum != expected {
		return nil, stage.Retryable(fmt.Errorf("fetch artifact hash mismatch: got %s, want %s", sum, expected))
	}
	return members, nil
}

func (s *Stage) resolveFrozenV1(ctx context.Context, env stage.Env, run stage.Run, item stage.Item, input store.GetExtractInputRow, members map[string]string) (selection, []vbpl.Node, []byte, error) {
	dir := env.Artifacts.Abs(input.Path)
	row, err := store.New(env.DB).GetRunOCRSelection(ctx, store.GetRunOCRSelectionParams{RunID: run.ID, ItemKey: item.Key})
	var selected selection
	var nodes []vbpl.Node
	var data []byte
	if err == nil {
		selected, nodes, data, err = s.verifyFrozenAuthority(ctx, dir, item.Key, input.Sha256, item.InputHash, row, members)
	} else if errors.Is(err, pgx.ErrNoRows) {
		selected, nodes, err = choose(ctx, dir, item.Key)
		if err == nil && selected.consulted {
			return selection{}, nil, nil, fmt.Errorf("missing frozen OCR selection")
		}
	} else {
		return selection{}, nil, nil, fmt.Errorf("read OCR selection: %w", err)
	}
	if err != nil {
		return selection{}, nil, nil, err
	}
	if observationInputHash(input.Sha256, selected) != item.InputHash {
		return selection{}, nil, nil, stage.Retryable(fmt.Errorf("text selection changed for %q", item.Key))
	}
	return selected, nodes, data, nil
}

func (s *Stage) verifyFrozenAuthority(ctx context.Context, dir, key, fetchHash, inputHash string, row store.GetRunOCRSelectionRow, members map[string]string) (selection, []vbpl.Node, []byte, error) {
	selected, err := selectionFromRowV1(row, fetchHash)
	if err != nil {
		return selection{}, nil, nil, err
	}
	decision, decisionNodes, err := choose(ctx, dir, key)
	if err != nil {
		return selection{}, nil, nil, err
	}
	if selected.gap == "text_number_mismatch" {
		if decision.gap != selected.gap {
			return selection{}, nil, nil, fmt.Errorf("frozen number mismatch conflicts with deterministic selection")
		}
	} else {
		if decision.gap == "text_number_mismatch" {
			return selection{}, nil, nil, fmt.Errorf("frozen OCR selection conflicts with number mismatch")
		}
		if (selected.ocrState == "checked" || selected.ocrState == "unavailable") && !decision.ocrReachable {
			return selection{}, nil, nil, fmt.Errorf("frozen OCR selection conflicts with tree or transcription precedence")
		}
		if selected.ocrState == "not_selected" && decision.ocrReachable {
			return selection{}, nil, nil, fmt.Errorf("frozen non-selection conflicts with OCR candidate")
		}
		if (selected.source == "none" && decision.source != "none") || (selected.source == "source_tree" && (decision.source != "source_tree" || decision.gap != selected.gap)) || (selected.source == "ocr" && decision.source != "none" && (decision.source != "source_tree" || !decision.ocrHeadingOnly)) {
			return selection{}, nil, nil, fmt.Errorf("frozen serving source conflicts with deterministic selection")
		}
	}
	if selected.entry != nil {
		member := "files/" + selected.hash
		if members[member] != selected.hash {
			return selection{}, nil, nil, fmt.Errorf("frozen OCR PDF is not a verified content-addressed member")
		}
		if !slices.Contains(s.engines, selected.entry.Text.EngineID) || selected.entry.Check.CheckerID != s.checker {
			return selection{}, nil, nil, fmt.Errorf("frozen OCR engine or checker differs from recipe")
		}
	}
	selected, nodes, data, err := verifyFrozenSelectionV1WithDecision(ctx, dir, key, fetchHash, inputHash, row, s.ocr, &decision, decisionNodes)
	if err != nil || selected.entry == nil {
		return selected, nodes, data, err
	}
	if err = s.validateFrozenServing(key, selected, decision.ocrHeadingOnly); err != nil {
		return selection{}, nil, nil, err
	}
	return selected, nodes, data, nil
}

func (s *Stage) validateFrozenServing(key string, selected selection, heading bool) error {
	if selected.verifiedCheck == nil || !selected.ocrParsed {
		return fmt.Errorf("missing verified OCR check or text")
	}
	result := *selected.verifiedCheck
	matched, _, err := matchWaiver(s.waivers.find(key), selected.hash, *selected.entry, result)
	if err != nil {
		return err
	}
	blocked := ""
	if result.Verdict == "rejected" {
		blocked = "ocr_rejected"
	} else {
		for _, item := range result.Items {
			if !matched && (item.Kind == "article_label" || item.Kind == "document_number") {
				blocked = "ocr_label_flagged"
				break
			}
		}
	}
	if selected.source == "none" {
		if blocked == "" || selected.gap != blocked {
			return fmt.Errorf("frozen OCR verdict and withheld gap disagree")
		}
	} else if selected.source == "ocr" && blocked != "" {
		return fmt.Errorf("frozen OCR verdict or waiver forbids serving")
	}
	if selected.source == "source_tree" {
		if blocked == "" && usableHeadingOCR(selected) {
			return fmt.Errorf("frozen heading fallback conflicts with usable OCR")
		}
		return nil
	}
	if heading && (selected.source != "ocr" || !usableHeadingOCR(selected)) {
		return fmt.Errorf("frozen OCR selection conflicts with heading fallback")
	}
	return nil
}

func usableHeadingOCR(selected selection) bool {
	return selected.ocrParseErr == nil && len(textparse.Parse(selected.ocrParagraphs).Provisions) > 0
}
