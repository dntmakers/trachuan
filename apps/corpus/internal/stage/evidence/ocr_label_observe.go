package evidence

import (
	"fmt"
	"math"
	"strconv"

	"danny.vn/trachuan/apps/corpus/internal/detector"
)

type ocrTuple struct {
	kind          string
	page, ordinal int
}

type ocrCoverage struct {
	tuples      map[ocrTuple]bool
	closeAbsent bool
}

func observeOCRLabelItems(inputs []detectorInput) (Observation, error) {
	var out Observation
	coverage := make(map[string]ocrCoverage, len(inputs))
	seen := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		doc := input.Document
		if doc.Key == "" || seen[doc.Key] {
			return Observation{}, &IncompleteError{Reason: "invalid or duplicate OCR document"}
		}
		seen[doc.Key] = true
		if doc.Baseline || !input.Observable {
			continue
		}
		if input.Detector == nil || input.Detector.Version != 1 {
			return Observation{}, &IncompleteError{Reason: fmt.Sprintf("missing detector inputs for %s", doc.Key)}
		}
		observation := input.Detector.OCRCheck
		switch observation.State {
		case "not_selected", "unavailable":
			if observation.OCRCheckDetails != nil {
				return Observation{}, &IncompleteError{Reason: fmt.Sprintf("unexpected OCR payload for %s", doc.Key)}
			}
			continue
		case "checked":
		default:
			return Observation{}, &IncompleteError{Reason: fmt.Sprintf("invalid OCR state for %s", doc.Key)}
		}
		check := observation.OCRCheckDetails
		if err := validateObservedOCR(check); err != nil {
			return Observation{}, &IncompleteError{Reason: fmt.Sprintf("invalid OCR observation for %s: %v", doc.Key, err)}
		}
		if check.OtherItems > math.MaxInt-out.Excluded {
			return Observation{}, &IncompleteError{Reason: "OCR exclusion count overflow"}
		}
		out.Excluded += check.OtherItems
		docCoverage := ocrCoverage{tuples: make(map[ocrTuple]bool, len(check.Items)), closeAbsent: check.SameDocument && (check.Verdict == "clean" || check.Verdict == "flagged") && !check.HasMissingText}
		for _, item := range check.Items {
			key := ocrTuple{kind: item.Kind, page: item.Page, ordinal: item.Ordinal}
			switch item.Position {
			case "ambiguous", "unlocated":
				out.NotObservable++
				docCoverage.tuples[key] = false
			case "tail", "not_tail":
				out.Observed++
				docCoverage.tuples[key] = true
				if item.Position == "tail" {
					location := item.Location
					out.Findings = append(out.Findings, Fired{
						Subject:           Subject{Type: "check_item", Fields: []string{doc.Key, item.Kind, strconv.Itoa(item.Page), strconv.Itoa(item.Ordinal)}},
						Severity:          detector.Review,
						FingerprintInputs: []string{check.PDFSHA256, check.TextSHA256, check.EngineID, check.CheckerID, check.Verdict, item.OCRReadingSHA256, item.PDFReadingSHA256, strconv.Itoa(location.TailScopeIndex), strconv.Itoa(location.StartParagraph), strconv.Itoa(location.EndParagraph)},
					})
				}
			}
		}
		coverage[doc.Key] = docCoverage
	}
	out.ObservedCompletely = func(subject Subject) bool {
		if subject.Type != "check_item" || len(subject.Fields) != 4 || (subject.Fields[1] != "article_label" && subject.Fields[1] != "document_number") {
			return false
		}
		page, pageErr := strconv.Atoi(subject.Fields[2])
		ordinal, ordinalErr := strconv.Atoi(subject.Fields[3])
		if pageErr != nil || ordinalErr != nil || page < 1 || ordinal < 1 || strconv.Itoa(page) != subject.Fields[2] || strconv.Itoa(ordinal) != subject.Fields[3] {
			return false
		}
		doc, ok := coverage[subject.Fields[0]]
		if !ok {
			return false
		}
		tuple := ocrTuple{kind: subject.Fields[1], page: page, ordinal: ordinal}
		if complete, found := doc.tuples[tuple]; found {
			return complete
		}
		return doc.closeAbsent
	}
	return out, nil
}
