package ocrstore

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

func VerifyStoredCheck(data []byte, expected string, flagged int32) error {
	_, err := VerifyStoredCheckResult(data, expected, flagged)
	return err
}

func VerifyStoredCheckResult(data []byte, expected string, flagged int32) (CheckResult, error) {
	result, err := DecodeCheck(data)
	if err != nil {
		return CheckResult{}, fmt.Errorf("decode stored check: %w", err)
	}
	if flagged < 0 || int64(len(result.Items)) != int64(flagged) || !validHash(expected) {
		return CheckResult{}, fmt.Errorf("invalid stored check identity or count")
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return CheckResult{}, fmt.Errorf("decode stored check fields: %w", err)
	}
	var canonical []byte
	if _, ok := fields["dropped_equal"]; ok {
		canonical, err = json.Marshal(result)
	} else {
		canonical, err = json.Marshal(struct {
			SameDocument bool        `json:"same_document"`
			Pages        int         `json:"pages"`
			PDFPages     int         `json:"pdf_pages"`
			Items        []CheckItem `json:"items"`
			Verdict      string      `json:"verdict"`
		}{result.SameDocument, result.Pages, result.PDFPages, result.Items, result.Verdict})
	}
	if err != nil {
		return CheckResult{}, fmt.Errorf("encode stored check: %w", err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(canonical)) != expected {
		return CheckResult{}, fmt.Errorf("stored check hash mismatch")
	}
	return result, nil
}
