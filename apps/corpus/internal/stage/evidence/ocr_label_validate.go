package evidence

import (
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

func validateObservedOCR(check *fetch.OCRCheckDetails) error {
	if check == nil || !detectorDigest(check.PDFSHA256) || !detectorDigest(check.TextSHA256) || !detectorDigest(check.ResultSHA256) || check.EngineID == "" || check.CheckerID == "" || check.PageCount < 1 || check.LineCount < 1 || check.ParagraphCount < 0 || check.ParagraphCount > check.LineCount || check.ScopeCount < 0 || check.ScopeCount > check.ParagraphCount || check.OtherItems < 0 || (check.HasMissingText && check.OtherItems == 0) || check.Items == nil {
		return fmt.Errorf("invalid check identity or counts")
	}
	switch check.Verdict {
	case "clean":
		if !check.SameDocument || len(check.Items) != 0 || check.OtherItems != 0 || check.HasMissingText {
			return fmt.Errorf("invalid clean check")
		}
	case "flagged":
		if !check.SameDocument || (len(check.Items) == 0 && check.OtherItems == 0) {
			return fmt.Errorf("invalid flagged check")
		}
	case "rejected":
		if check.SameDocument && !check.HasMissingText {
			return fmt.Errorf("invalid rejected check")
		}
	default:
		return fmt.Errorf("invalid check verdict")
	}
	type itemKey struct {
		kind string
		page int
	}
	ordinals := make(map[itemKey]int)
	for _, item := range check.Items {
		if (item.Kind != "article_label" && item.Kind != "document_number") || item.Page < 1 || item.Page > check.PageCount || !detectorDigest(item.OCRReadingSHA256) || !detectorDigest(item.PDFReadingSHA256) {
			return fmt.Errorf("invalid label identity")
		}
		key := itemKey{kind: item.Kind, page: item.Page}
		ordinals[key]++
		if item.Ordinal != ordinals[key] {
			return fmt.Errorf("invalid label ordinal")
		}
		if !check.SameDocument && item.Position != "unlocated" {
			return fmt.Errorf("wrong-document item is located")
		}
		switch item.Position {
		case "tail":
			if err := fetch.ValidateOCRLocation(item.Location, *check); err != nil {
				return fmt.Errorf("invalid tail location: %w", err)
			}
		case "not_tail", "ambiguous", "unlocated":
			if item.Location != nil || (check.ScopeCount == 0 && item.Position != "unlocated") {
				return fmt.Errorf("invalid label position")
			}
		default:
			return fmt.Errorf("invalid label position")
		}
	}
	return nil
}
