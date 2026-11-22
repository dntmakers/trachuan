package extract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
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
		if err == nil && selected.gap != "text_number_mismatch" {
			var mismatch selection
			mismatch, err = frozenMismatch(dir, item.Key)
			if err == nil && mismatch.gap != "" {
				err = fmt.Errorf("frozen OCR selection conflicts with number mismatch")
			}
		}
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
	if selected.entry != nil {
		member := "files/" + selected.hash
		if members[member] != selected.hash {
			return selection{}, nil, nil, fmt.Errorf("frozen OCR PDF is not a verified content-addressed member")
		}
		if !slices.Contains(s.engines, selected.entry.Text.EngineID) || selected.entry.Check.CheckerID != s.checker {
			return selection{}, nil, nil, fmt.Errorf("frozen OCR engine or checker differs from recipe")
		}
	}
	selected, nodes, data, err := verifyFrozenSelectionV1(ctx, dir, key, fetchHash, inputHash, row, s.ocr)
	if err != nil || selected.entry == nil {
		return selected, nodes, data, err
	}
	if err = s.validateFrozenServing(ctx, dir, key, selected, data); err != nil {
		return selection{}, nil, nil, err
	}
	return selected, nodes, data, nil
}

func (s *Stage) validateFrozenServing(ctx context.Context, dir, key string, selected selection, data []byte) error {
	result, err := ocrstore.DecodeCheck(selected.entry.Check.Result)
	if err != nil {
		return fmt.Errorf("decode frozen OCR check: %w", err)
	}
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
		if blocked == "" && usableHeadingOCR(data) {
			return fmt.Errorf("frozen heading fallback conflicts with usable OCR")
		}
		return nil
	}
	heading, err := frozenOCRHeadingOnly(ctx, dir, key)
	if err != nil {
		return err
	}
	if heading && (selected.source != "ocr" || !usableHeadingOCR(data)) {
		return fmt.Errorf("frozen OCR selection conflicts with heading fallback")
	}
	return nil
}

func usableHeadingOCR(data []byte) bool {
	paragraphs, err := textparse.ParagraphsFromText(string(data))
	return err == nil && len(textparse.Parse(paragraphs).Provisions) > 0
}

func frozenOCRHeadingOnly(ctx context.Context, dir, key string) (bool, error) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 || parts[1] != "vbpl" {
		return false, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "tree.json"))
	if err != nil {
		return false, fmt.Errorf("read frozen OCR tree: %w", err)
	}
	nodes, err := vbpl.DecodeTree(ctx, strings.NewReader(string(data)), vbpl.MaxTreeDepth, vbpl.MaxTreeNodes)
	if err != nil {
		return false, err
	}
	if len(nodes) == 0 {
		return false, nil
	}
	rows, _, err := extractTree(ctx, nodes)
	if err != nil {
		return false, err
	}
	if sourceHeadingCount(rows, nodes) == 0 || !headingOnly(rows) {
		return false, fmt.Errorf("frozen OCR conflicts with source tree selection")
	}
	return true, nil
}
