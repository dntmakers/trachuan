package extract

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
	"danny.vn/trachuan/apps/corpus/internal/textparse"
)

type ocrCheckObservation = fetch.OCRCheckObservation
type ocrCheckDetails = fetch.OCRCheckDetails
type ocrCheckItem = fetch.OCRCheckItem
type ocrLocationRecord = fetch.OCRLocation

func buildOCRCheckObservation(selected selection, data []byte) (ocrCheckObservation, error) {
	state := selected.ocrState
	if state == "" {
		switch {
		case selected.entry != nil:
			state = "checked"
		case selected.consulted && selected.gap != "text_number_mismatch":
			state = "unavailable"
		default:
			state = "not_selected"
		}
	}
	if state == "not_selected" || state == "unavailable" {
		if selected.entry != nil {
			return ocrCheckObservation{}, fmt.Errorf("unexpected OCR check for %s", state)
		}
		return ocrCheckObservation{State: state}, nil
	}
	if state != "checked" || selected.entry == nil || selected.entry.Check == nil {
		return ocrCheckObservation{}, fmt.Errorf("invalid OCR observation state")
	}
	entry := selected.entry
	if err := ocrstore.VerifyStoredCheck(entry.Check.Result, entry.Check.ResultSha256, entry.Check.Flagged); err != nil {
		return ocrCheckObservation{}, fmt.Errorf("verify OCR observation check: %w", err)
	}
	check, err := ocrstore.DecodeCheck(entry.Check.Result)
	if err != nil {
		return ocrCheckObservation{}, fmt.Errorf("decode OCR observation check: %w", err)
	}
	details := &ocrCheckDetails{
		PDFSHA256: selected.hash, EngineID: entry.Text.EngineID, TextSHA256: entry.Text.TextSha256,
		CheckerID: entry.Check.CheckerID, ResultSHA256: entry.Check.ResultSha256,
		SameDocument: check.SameDocument, Verdict: check.Verdict, PageCount: check.PDFPages,
		LineCount: len(strings.Split(string(data), "\n")), Items: make([]ocrCheckItem, 0),
	}
	paragraphs, parseErr := textparse.ParagraphsFromText(string(data))
	if parseErr == nil {
		details.ParagraphCount = len(paragraphs)
		details.ScopeCount = len(textparse.ParseArticles(paragraphs).Scopes)
	}
	var readings []string
	for _, item := range check.Items {
		if item.Kind == "article_label" || item.Kind == "document_number" {
			readings = append(readings, item.OCR)
		} else {
			details.OtherItems++
			if item.Kind == "missing_text" {
				details.HasMissingText = true
			}
		}
	}
	positions := make([]ocrReadingPosition, len(readings))
	for i := range positions {
		positions[i].Position = "unlocated"
	}
	if check.SameDocument && parseErr == nil {
		positions = locateOCRReadings(string(data), readings, ocrReadingBudget)
	}
	type itemKey struct {
		kind string
		page int
	}
	ordinals := make(map[itemKey]int)
	for _, item := range check.Items {
		if item.Kind != "article_label" && item.Kind != "document_number" {
			continue
		}
		key := itemKey{kind: item.Kind, page: item.Page}
		ordinals[key]++
		position := positions[len(details.Items)]
		entry := ocrCheckItem{Kind: item.Kind, Page: item.Page, Ordinal: ordinals[key], OCRReadingSHA256: readingHash(item.OCR), PDFReadingSHA256: readingHash(item.PDF), Position: position.Position}
		if position.Location != nil {
			loc := position.Location
			entry.Location = &ocrLocationRecord{TailScopeIndex: loc.TailScopeIndex, StartParagraph: loc.StartParagraph, EndParagraph: loc.EndParagraph, StartLine: loc.StartLine, EndLine: loc.EndLine}
		}
		details.Items = append(details.Items, entry)
	}
	return ocrCheckObservation{State: "checked", OCRCheckDetails: details}, nil
}

func readingHash(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(normalizeOCRReading(value))))
}
