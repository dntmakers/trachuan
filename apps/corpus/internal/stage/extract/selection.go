package extract

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
	"danny.vn/trachuan/apps/corpus/internal/textparse"
	"danny.vn/trachuan/apps/corpus/internal/vbpl"
)

type ocrSource interface{ available(string) bool }
type absentOCR struct{}

func (absentOCR) available(string) bool { return false }

type treeTranscriptionObservation struct {
	State                                                            string
	TreeChars, TranscriptionChars                                    int
	TreeTextSHA256, TranscriptionTextSHA256, TranscriptionFileSHA256 string
	MainAccepted                                                     bool
}

type selection struct {
	treeTranscription       treeTranscriptionObservation
	ocrState                string
	source, gap, name, hash string
	reason                  string
	extraGap, extraReason   string
	data                    []byte
	tooLarge, parseLimit    bool
	consulted               bool
	entry                   *ocrstore.Entry
	tree                    *treeComparison
}

func InputHash(fetchHash, source string, details ...string) string {
	return fetch.ExtractInputHash(fetchHash, source, details...)
}

func selectionHash(hash, source string) string { return InputHash(hash, source) }

func selectFiles(dir, id string, files []fetch.DocumentFile, ocr ocrSource) (selection, error) {
	for _, f := range files {
		if f.Name != id+"_content.html" {
			continue
		}
		if len(f.SHA256) != 64 {
			return selection{}, fmt.Errorf("invalid transcription hash")
		}
		if _, err := hex.DecodeString(f.SHA256); err != nil {
			return selection{}, fmt.Errorf("invalid transcription hash: %w", err)
		}
		info, err := os.Lstat(filepath.Join(dir, "files", f.SHA256))
		if err != nil {
			return selection{}, fmt.Errorf("stat transcription: %w", err)
		}
		if !info.Mode().IsRegular() {
			return selection{}, fmt.Errorf("transcription is not a regular file")
		}
		if f.Size > 50_000_000 || info.Size() > 50_000_000 {
			return selection{source: "html_transcription", name: f.Name, hash: f.SHA256, tooLarge: true}, nil
		}
		data, err := os.ReadFile(filepath.Join(dir, "files", f.SHA256))
		if err != nil {
			return selection{}, fmt.Errorf("read transcription: %w", err)
		}
		paragraphs, err := textparse.ParagraphsFromHTML(data)
		var limit *textparse.LimitError
		if err != nil && !errors.As(err, &limit) {
			return selection{}, fmt.Errorf("read transcription paragraphs: %w", err)
		}
		count := 0
		for _, p := range paragraphs {
			count += utf8.RuneCountInString(p.Text)
		}
		if count >= 300 || limit != nil {
			return selection{source: "html_transcription", name: f.Name, hash: f.SHA256, data: data, parseLimit: limit != nil}, nil
		}
	}
	pdfs := map[string]bool{}
	usable := 0
	unsupported := false
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		switch ext {
		case ".pdf":
			pdfs[f.SHA256] = true
		case ".doc", ".rar", ".zip", ".7z", ".tar", ".gz":
			unsupported = true
		}
	}
	for hash := range pdfs {
		if ocr.available(hash) {
			usable++
		}
	}
	gap := "no_text_source"
	switch {
	case len(pdfs) == 1 || usable == 1:
		gap = "needs_ocr"
	case len(pdfs) > 1:
		gap = "text_source_ambiguous"
	case unsupported:
		gap = "file_format_unsupported"
	}
	return selection{source: "none", gap: gap}, nil
}
func choose(ctx context.Context, dir, key string) (selection, []vbpl.Node, error) {
	return chooseWithOCR(ctx, dir, key, nil, nil, "")
}

func chooseWithOCR(ctx context.Context, dir, key string, ocr ocrReader, engines []string, checker string) (selection, []vbpl.Node, error) {
	return chooseForCheck(ctx, dir, key, ocr, engines, checker, false)
}

func chooseForCheck(ctx context.Context, dir, key string, ocr ocrReader, engines []string, checker string, unchecked bool, waivers ...*Waiver) (selection, []vbpl.Node, error) {
	selected, nodes, err := chooseForCheckRaw(ctx, dir, key, ocr, engines, checker, unchecked, waivers...)
	setTreeTranscriptionDefault(key, &selected)
	return selected, nodes, err
}

func setTreeTranscriptionDefault(key string, selected *selection) {
	if selected.treeTranscription.State == "" {
		selected.treeTranscription.State = "no_pair"
		if parts := strings.SplitN(key, ":", 3); len(parts) == 3 && parts[1] != "vbpl" {
			selected.treeTranscription.State = "not_applicable"
		}
	}
}

func chooseForCheckRaw(ctx context.Context, dir, key string, ocr ocrReader, engines []string, checker string, unchecked bool, waivers ...*Waiver) (selection, []vbpl.Node, error) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		return selection{}, nil, fmt.Errorf("invalid document key")
	}
	if parts[1] == "vbpl" {
		mismatch, err := frozenMismatch(dir, key)
		if err != nil {
			return selection{}, nil, err
		}
		if mismatch.gap != "" {
			return mismatch, nil, nil
		}
		b, err := os.ReadFile(filepath.Join(dir, "tree.json"))
		if err != nil {
			return selection{}, nil, fmt.Errorf("read tree: %w", err)
		}
		nodes, err := vbpl.DecodeTree(ctx, strings.NewReader(string(b)), vbpl.MaxTreeDepth, vbpl.MaxTreeNodes)
		if err != nil {
			return selection{}, nil, err
		}
		if len(nodes) > 0 {
			rows, _, extractErr := extractTree(ctx, nodes)
			if extractErr != nil {
				return selection{}, nil, extractErr
			}
			headingCount := sourceHeadingCount(rows, nodes)
			if headingCount == 0 {
				data, readErr := os.ReadFile(filepath.Join(dir, "files.json"))
				if errors.Is(readErr, os.ErrNotExist) {
					return selection{source: "source_tree"}, nodes, nil
				}
				if readErr != nil {
					return selection{}, nil, fmt.Errorf("read file list: %w", readErr)
				}
				var files []fetch.DocumentFile
				if readErr = json.Unmarshal(data, &files); readErr != nil {
					return selection{}, nil, fmt.Errorf("decode file list: %w", readErr)
				}
				transcription, selectErr := selectFiles(dir, parts[2], files, absentOCR{})
				if selectErr != nil {
					return selection{}, nil, selectErr
				}
				return partialTreeSelection(rows, nodes, transcription, selection{source: "source_tree"})
			}
			doc, readErr := fetch.ReadDocument(dir, key)
			if readErr != nil {
				return selection{}, nil, fmt.Errorf("read fetch document: %w", readErr)
			}
			if headingCount > 0 && headingOnly(rows) {
				selected, selectErr := selectOCRFilesMode(ctx, dir, parts[2], doc.Files, ocr, engines, checker, unchecked, waivers...)
				if selectErr != nil {
					return selection{}, nil, selectErr
				}
				if selected.source == "ocr" {
					data, readErr := ocr.ReadText(selected.entry.Text)
					if readErr == nil {
						paragraphs, parseErr := textparse.ParagraphsFromText(string(data))
						if parseErr != nil || len(textparse.Parse(paragraphs).Provisions) == 0 {
							selected.parseLimit = true
						}
					}
				}
				if selected.source != "none" && !selected.tooLarge && !selected.parseLimit {
					if selected.source == "html_transcription" {
						tree, text, accepted, usable := partialTreeTexts(rows, selected)
						selected.treeTranscription = treeTranscriptionValue(tree, text, accepted, usable, selected)
					}
					return selected, nil, nil
				}
				state := "not_selected"
				if selected.consulted {
					state = "unavailable"
				}
				if selected.entry != nil && selected.entry.Check != nil {
					state = "checked"
				}
				return selection{source: "source_tree", gap: "tree_headings_only", hash: selected.hash, entry: selected.entry, ocrState: state, consulted: selected.consulted || selected.source == "none"}, nodes, nil
			}
			transcription, selectErr := selectFiles(dir, parts[2], doc.Files, absentOCR{})
			if selectErr != nil {
				return selection{}, nil, selectErr
			}
			comparison := compareTree(rows, transcription)
			comparison.HeadingOnlyArticles = headingCount
			if comparison.SetAside {
				transcription.tree = comparison
				tree, text, accepted, usable := partialTreeTexts(rows, transcription)
				transcription.treeTranscription = treeTranscriptionValue(tree, text, accepted, usable, transcription)
				return transcription, nil, nil
			}
			gap := "tree_headings_partial"
			if comparison.Articles == comparison.HeadingOnlyArticles {
				gap = "tree_headings_only"
			}
			return partialTreeSelection(rows, nodes, transcription, selection{source: "source_tree", gap: gap, reason: comparison.detail()})
		}
	}
	doc, err := fetch.ReadDocument(dir, key)
	if err != nil {
		return selection{}, nil, fmt.Errorf("read fetch document: %w", err)
	}
	s, err := selectOCRFilesMode(ctx, dir, parts[2], doc.Files, ocr, engines, checker, unchecked, waivers...)
	return s, nil, err
}

func headingOnly(rows []provision) bool {
	children := make(map[string]bool)
	for _, row := range rows {
		if row.ParentNodeID != nil {
			children[*row.ParentNodeID] = true
		}
	}
	found := false
	for _, row := range rows {
		if row.Kind == "dieu" {
			found = true
		}
		if row.Kind == "dieu" && children[row.NodeID] {
			return false
		}
		if !headingTextOnly(row) {
			return false
		}
	}
	return found
}
