package classify

import (
	"bytes"
	"encoding/json"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

func isOpenAIBatchZeroWork(batch openAIBatchTerminal) bool {
	if !batch.CountsUsable || batch.UsageState == openAIAggregateInvalid || batch.Status != "failed" || batch.Total != 0 || batch.Completed != 0 || batch.Failed != 0 || batch.OutputFileID != "" || batch.ErrorFileID != "" {
		return false
	}
	fields, err := batchapi.DecodeEvidenceProjection(batch.Raw, "in_progress_at", "finalizing_at", "completed_at", "expired_at", "cancelling_at", "cancelled_at", "errors", "usage")
	if err != nil {
		return false
	}
	for _, name := range []string{"in_progress_at", "finalizing_at", "completed_at", "expired_at", "cancelling_at", "cancelled_at"} {
		if len(fields[name]) != 0 && !bytes.Equal(fields[name], []byte("null")) {
			return false
		}
	}
	errors, err := batchapi.DecodeEvidenceProjection(fields["errors"], "object", "data")
	if err != nil {
		return false
	}
	kind, err := evidenceString(errors["object"])
	if err != nil || kind != "list" {
		return false
	}
	var entries []json.RawMessage
	if json.Unmarshal(errors["data"], &entries) != nil || len(entries) == 0 {
		return false
	}
	for _, entry := range entries {
		errorFields, err := batchapi.DecodeEvidenceProjection(entry, "code", "line", "message", "param")
		if err != nil {
			return false
		}
		code, err := evidenceString(errorFields["code"])
		if err != nil || code == "" {
			return false
		}
		for _, name := range []string{"message", "param"} {
			if len(errorFields[name]) > 0 && !bytes.Equal(errorFields[name], []byte("null")) {
				if _, err := evidenceString(errorFields[name]); err != nil {
					return false
				}
			}
		}
		if line := errorFields["line"]; len(line) > 0 && !bytes.Equal(line, []byte("null")) {
			if _, ok := batchapi.TokenInteger(line, 0); !ok {
				return false
			}
		}
	}
	if len(fields["usage"]) == 0 {
		return true
	}
	return openAIZeroAggregateUsage(fields["usage"])
}

func openAIValidAggregateUsage(raw json.RawMessage) bool {
	return openAICheckAggregateUsage(raw, false)
}

func openAIZeroAggregateUsage(raw json.RawMessage) bool {
	return openAICheckAggregateUsage(raw, true)
}

func openAICheckAggregateUsage(raw json.RawMessage, zero bool) bool {
	fields, err := batchapi.DecodeEvidenceObject(raw, "input_tokens", "output_tokens", "total_tokens", "input_tokens_details", "output_tokens_details")
	if err != nil {
		return false
	}
	for key := range fields {
		switch key {
		case "input_tokens", "output_tokens", "total_tokens", "input_tokens_details", "output_tokens_details":
		default:
			return false
		}
	}
	for _, name := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		n, ok := batchapi.TokenInteger(fields[name], 0)
		if !ok || (zero && n != 0) {
			return false
		}
	}
	for _, detail := range []struct{ name, field string }{{"input_tokens_details", "cached_tokens"}, {"output_tokens_details", "reasoning_tokens"}} {
		if len(fields[detail.name]) == 0 {
			continue
		}
		object, err := batchapi.DecodeEvidenceObject(fields[detail.name], detail.field)
		if err != nil || len(object) != 1 {
			return false
		}
		n, ok := batchapi.TokenInteger(object[detail.field], 0)
		if !ok || (zero && n != 0) {
			return false
		}
	}
	return true
}
