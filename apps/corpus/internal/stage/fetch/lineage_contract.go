package fetch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func ExtractObservationInputHash(base, pdfSHA256, engineID, textSHA256, checkerID, resultSHA256 string) string {
	return stage.RecipeHash("extract-ocr-observation", "1", base, pdfSHA256, engineID, textSHA256, checkerID, resultSHA256)
}

func CheckExtractLineageContract(dir, fetchHash, inputHash, contract string) error {
	switch contract {
	case "legacy":
		data, err := os.ReadFile(filepath.Join(dir, "text.json"))
		if errors.Is(err, os.ErrNotExist) {
			return CheckExtractLineage(dir, fetchHash, inputHash)
		}
		if err != nil {
			return fmt.Errorf("read extract text record: %w", err)
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(data, &fields); err != nil {
			return fmt.Errorf("decode extract text record: %w", err)
		}
		if _, ok := fields["detector_inputs"]; ok {
			return fmt.Errorf("legacy contract rejects detector inputs")
		}
		return CheckExtractLineage(dir, fetchHash, inputHash)
	case "detector-inputs/1":
		return checkExtractLineageV1(dir, fetchHash, inputHash)
	default:
		return fmt.Errorf("unknown extract record contract")
	}
}

func checkExtractLineageV1(dir, fetchHash, inputHash string) error {
	data, err := os.ReadFile(filepath.Join(dir, "text.json"))
	if err != nil {
		return fmt.Errorf("read extract text record: %w", err)
	}
	fields, err := detectorObjectMode(data, true)
	if err != nil {
		return fmt.Errorf("decode extract text record: %w", err)
	}
	source, err := lineageString(fields, "text_source")
	if err != nil {
		return err
	}
	detector, err := DecodeDetectorInputsV1(fields["detector_inputs"], source)
	if err != nil {
		return fmt.Errorf("decode detector inputs: %w", err)
	}
	base := ExtractInputHash(fetchHash, source)
	switch source {
	case "none":
		gap, gapErr := lineageString(fields, "gap")
		if gapErr != nil {
			return gapErr
		}
		switch gap {
		case "needs_ocr", "text_source_ambiguous", "file_format_unsupported", "no_text_source", "ocr_rejected", "ocr_label_flagged", "text_number_mismatch":
		default:
			return fmt.Errorf("invalid absent text gap")
		}
		if detector.OCRCheck.State == "checked" {
			if (gap == "ocr_rejected" && detector.OCRCheck.Verdict != "rejected") || (gap == "ocr_label_flagged" && detector.OCRCheck.Verdict != "flagged") || (gap != "ocr_rejected" && gap != "ocr_label_flagged") {
				return fmt.Errorf("OCR gap and check disagree")
			}
		} else if gap == "ocr_rejected" || gap == "ocr_label_flagged" {
			return fmt.Errorf("OCR gap requires a check")
		}
		base = ExtractInputHash(fetchHash, source, gap)
	case "ocr":
		check := detector.OCRCheck.OCRCheckDetails
		for key, want := range map[string]string{"sha256": check.PDFSHA256, "engine_id": check.EngineID, "text_sha256": check.TextSHA256, "checker_id": check.CheckerID, "result_sha256": check.ResultSHA256} {
			value, fieldErr := lineageString(fields, key)
			if fieldErr != nil || value != want {
				return fmt.Errorf("OCR lineage field %s disagrees with check", key)
			}
		}
		base = ExtractInputHash(fetchHash, source, check.TextSHA256, check.ResultSHA256)
	}
	expected := base
	if detector.OCRCheck.State == "checked" {
		check := detector.OCRCheck.OCRCheckDetails
		expected = ExtractObservationInputHash(base, check.PDFSHA256, check.EngineID, check.TextSHA256, check.CheckerID, check.ResultSHA256)
	}
	if expected != inputHash {
		return fmt.Errorf("artifact input hash mismatch")
	}
	return nil
}

func lineageString(fields map[string]json.RawMessage, name string) (string, error) {
	value, ok := fields[name]
	if !ok || string(value) == "null" {
		return "", fmt.Errorf("missing extract field %s", name)
	}
	var result string
	if err := json.Unmarshal(value, &result); err != nil || result == "" {
		return "", fmt.Errorf("invalid extract field %s", name)
	}
	return result, nil
}
