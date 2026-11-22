package fetch

import (
	"encoding/json"
	"fmt"
)

func DecodeDetectorInputsV1(data []byte, source string) (DetectorInputsV1, error) {
	var result DetectorInputsV1
	fields, err := detectorObject(data)
	if err != nil {
		return result, fmt.Errorf("detector inputs: %w", err)
	}
	if err = detectorFields(fields, "version", "ocr_check", "tree_transcription"); err != nil {
		return result, fmt.Errorf("detector inputs: %w", err)
	}
	if err = json.Unmarshal(fields["version"], &result.Version); err != nil || result.Version != 1 {
		return result, fmt.Errorf("invalid detector version")
	}
	if result.OCRCheck, err = decodeOCRObservation(fields["ocr_check"]); err != nil {
		return DetectorInputsV1{}, fmt.Errorf("OCR observation: %w", err)
	}
	if result.TreeTranscription, err = decodeTreeObservation(fields["tree_transcription"]); err != nil {
		return DetectorInputsV1{}, fmt.Errorf("tree observation: %w", err)
	}
	if err = validateDetectorSource(result, source); err != nil {
		return DetectorInputsV1{}, err
	}
	return result, nil
}

func decodeOCRObservation(data []byte) (OCRCheckObservation, error) {
	var result OCRCheckObservation
	fields, err := detectorObject(data)
	if err != nil {
		return result, err
	}
	state, err := detectorState(fields)
	if err != nil {
		return result, err
	}
	result.State = state
	switch state {
	case "not_selected", "unavailable":
		return result, detectorFields(fields, "state")
	case "checked":
		if err = detectorFields(fields, "state", "pdf_sha256", "engine_id", "text_sha256", "checker_id", "result_sha256", "same_document", "verdict", "has_missing_text", "page_count", "paragraph_count", "line_count", "scope_count", "other_items", "items"); err != nil {
			return result, err
		}
	default:
		return result, fmt.Errorf("unknown OCR state")
	}
	var items []json.RawMessage
	if err = json.Unmarshal(fields["items"], &items); err != nil || items == nil {
		return result, fmt.Errorf("invalid OCR items")
	}
	for _, item := range items {
		if err = validateOCRItemObject(item); err != nil {
			return result, err
		}
	}
	var details OCRCheckDetails
	if err = json.Unmarshal(data, &details); err != nil {
		return result, fmt.Errorf("decode OCR fields: %w", err)
	}
	if err = validateOCRDetails(details); err != nil {
		return result, err
	}
	result.OCRCheckDetails = &details
	return result, nil
}

func validateOCRItemObject(data []byte) error {
	fields, err := detectorObject(data)
	if err != nil {
		return err
	}
	required := []string{"kind", "page", "ordinal", "ocr_reading_sha256", "pdf_reading_sha256", "position"}
	var position string
	if err = json.Unmarshal(fields["position"], &position); err != nil {
		return fmt.Errorf("invalid OCR item position")
	}
	if position == "tail" {
		required = append(required, "location")
		location, locErr := detectorObject(fields["location"])
		if locErr != nil {
			return fmt.Errorf("invalid OCR location: %w", locErr)
		}
		if locErr = detectorFields(location, "tail_scope_index", "start_paragraph", "end_paragraph", "start_line", "end_line"); locErr != nil {
			return fmt.Errorf("invalid OCR location: %w", locErr)
		}
	}
	return detectorFields(fields, required...)
}

func decodeTreeObservation(data []byte) (TreeTranscriptionV1, error) {
	var result TreeTranscriptionV1
	fields, err := detectorObject(data)
	if err != nil {
		return result, err
	}
	state, err := detectorState(fields)
	if err != nil {
		return result, err
	}
	result.State = state
	switch state {
	case "not_applicable", "no_pair":
		return result, detectorFields(fields, "state")
	case "measured":
		if err = detectorFields(fields, "state", "tree_chars", "transcription_chars", "tree_text_sha256", "transcription_text_sha256", "transcription_file_sha256", "main_accepted"); err != nil {
			return result, err
		}
	default:
		return result, fmt.Errorf("unknown tree state")
	}
	var details TreeTranscriptionDetails
	if err = json.Unmarshal(data, &details); err != nil {
		return result, fmt.Errorf("decode tree fields: %w", err)
	}
	if details.TreeChars <= 0 || details.TranscriptionChars <= 0 || !detectorHash(details.TreeTextSHA256) || !detectorHash(details.TranscriptionTextSHA256) || !detectorHash(details.TranscriptionFileSHA256) {
		return result, fmt.Errorf("invalid measured tree")
	}
	result.TreeTranscriptionDetails = &details
	return result, nil
}
