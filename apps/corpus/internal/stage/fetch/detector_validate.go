package fetch

import (
	"encoding/hex"
	"fmt"
	"strings"
)

func detectorHash(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateOCRDetails(d OCRCheckDetails) error {
	if !detectorHash(d.PDFSHA256) || !detectorHash(d.TextSHA256) || !detectorHash(d.ResultSHA256) || d.EngineID == "" || d.CheckerID == "" {
		return fmt.Errorf("invalid OCR identity")
	}
	if d.PageCount < 1 || d.LineCount < 1 || d.ParagraphCount < 0 || d.ParagraphCount > d.LineCount || d.ScopeCount < 0 || d.ScopeCount > d.ParagraphCount || d.OtherItems < 0 || (d.HasMissingText && d.OtherItems == 0) {
		return fmt.Errorf("invalid OCR counts")
	}
	switch d.Verdict {
	case "clean":
		if !d.SameDocument || len(d.Items) != 0 || d.OtherItems != 0 || d.HasMissingText {
			return fmt.Errorf("invalid clean OCR result")
		}
	case "flagged":
		if !d.SameDocument || (len(d.Items) == 0 && d.OtherItems == 0) {
			return fmt.Errorf("invalid flagged OCR result")
		}
	case "rejected":
		if d.SameDocument && !d.HasMissingText {
			return fmt.Errorf("invalid rejected OCR result")
		}
	default:
		return fmt.Errorf("invalid OCR verdict")
	}
	type itemKey struct {
		kind string
		page int
	}
	ordinals := make(map[itemKey]int)
	for _, item := range d.Items {
		if (item.Kind != "article_label" && item.Kind != "document_number") || item.Page < 1 || item.Page > d.PageCount || !detectorHash(item.OCRReadingSHA256) || !detectorHash(item.PDFReadingSHA256) {
			return fmt.Errorf("invalid OCR item identity")
		}
		key := itemKey{kind: item.Kind, page: item.Page}
		ordinals[key]++
		if item.Ordinal != ordinals[key] {
			return fmt.Errorf("invalid OCR item ordinal")
		}
		if !d.SameDocument && item.Position != "unlocated" {
			return fmt.Errorf("wrong-document OCR position")
		}
		switch item.Position {
		case "tail":
			if err := validateOCRLocation(item.Location, d); err != nil {
				return err
			}
		case "not_tail", "ambiguous", "unlocated":
			if item.Location != nil {
				return fmt.Errorf("unexpected OCR location")
			}
		default:
			return fmt.Errorf("invalid OCR position")
		}
		if d.ScopeCount == 0 && item.Position != "unlocated" {
			return fmt.Errorf("OCR position without scopes")
		}
	}
	return nil
}

func validateOCRLocation(loc *OCRLocation, d OCRCheckDetails) error {
	if loc == nil || loc.TailScopeIndex < 1 || loc.TailScopeIndex >= d.ScopeCount || loc.StartParagraph < 1 || loc.EndParagraph < loc.StartParagraph || loc.EndParagraph > d.ParagraphCount || loc.StartLine < 1 || loc.EndLine < loc.StartLine || loc.EndLine > d.LineCount || loc.StartLine < loc.StartParagraph || loc.EndLine-loc.StartLine < loc.EndParagraph-loc.StartParagraph {
		return fmt.Errorf("invalid OCR tail location")
	}
	return nil
}

func validateDetectorSource(value DetectorInputsV1, source string) error {
	switch source {
	case "":
		return nil
	case "ocr":
		if value.OCRCheck.State != "checked" || value.OCRCheck.Verdict == "rejected" {
			return fmt.Errorf("OCR source requires a non-rejected check")
		}
	case "html_transcription":
		if value.OCRCheck.State == "checked" {
			return fmt.Errorf("transcription cannot carry an OCR check")
		}
	case "none":
		if value.OCRCheck.State == "checked" && (value.OCRCheck.Verdict == "clean" || (value.OCRCheck.Verdict == "flagged" && len(value.OCRCheck.Items) == 0)) {
			return fmt.Errorf("withheld OCR requires a rejected or label-flagged check")
		}
	case "source_tree":
	default:
		return fmt.Errorf("unknown text source")
	}
	if value.TreeTranscription.State == "measured" && source != "source_tree" && source != "html_transcription" {
		return fmt.Errorf("measured tree conflicts with text source")
	}
	return nil
}
