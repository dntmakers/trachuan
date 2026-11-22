package classify

import (
	"bytes"
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

type openAIBatchIdentity struct{ BatchID, SubmissionID, InputFileID string }

type openAIAggregateState uint8

const (
	openAIAggregateInvalid openAIAggregateState = iota
	openAIAggregateAbsent
	openAIAggregatePresent
)

type openAIBatchTerminal struct {
	Identity                  openAIBatchIdentity
	Status                    string
	Total, Completed, Failed  int64
	CountsUsable              bool
	UsageState                openAIAggregateState
	OutputFileID, ErrorFileID string
	Raw, Usage                json.RawMessage
}

func parseOpenAIBatchTerminal(raw []byte, identity openAIBatchIdentity) (openAIBatchTerminal, error) {
	var result openAIBatchTerminal
	fail := func() (openAIBatchTerminal, error) {
		return openAIBatchTerminal{}, fmt.Errorf("invalid terminal batch evidence")
	}
	if identity.BatchID == "" || identity.SubmissionID == "" || identity.InputFileID == "" {
		return fail()
	}
	fields, err := batchapi.DecodeEvidenceProjection(raw, "id", "endpoint", "input_file_id", "metadata", "status", "output_file_id", "error_file_id")
	if err != nil {
		return fail()
	}
	for name, expected := range map[string]string{"id": identity.BatchID, "endpoint": "/v1/responses", "input_file_id": identity.InputFileID} {
		value, e := evidenceString(fields[name])
		if e != nil || value != expected {
			return fail()
		}
	}
	metadata, err := batchapi.DecodeEvidenceProjection(fields["metadata"], "submission_id")
	if err != nil {
		return fail()
	}
	submission, err := evidenceString(metadata["submission_id"])
	if err != nil || submission != identity.SubmissionID {
		return fail()
	}
	status, err := evidenceString(fields["status"])
	if err != nil {
		return fail()
	}
	switch status {
	case "completed", "failed", "expired", "cancelled":
	default:
		return fail()
	}
	ids := make([]string, 2)
	for i, name := range []string{"output_file_id", "error_file_id"} {
		field := fields[name]
		if len(field) == 0 || bytes.Equal(field, []byte("null")) {
			continue
		}
		ids[i], err = evidenceString(field)
		if err != nil || ids[i] == "" {
			return fail()
		}
	}
	if ids[0] != "" && ids[0] == ids[1] {
		return fail()
	}
	result = openAIBatchTerminal{Identity: identity, Status: status, OutputFileID: ids[0], ErrorFileID: ids[1], Raw: append(json.RawMessage(nil), raw...)}
	result.Total, result.Completed, result.Failed, result.CountsUsable = openAIBatchCounts(raw)
	if usage, usageErr := batchapi.DecodeEvidenceProjection(raw, "usage"); usageErr == nil {
		result.Usage = append(json.RawMessage(nil), usage["usage"]...)
		if len(result.Usage) == 0 {
			result.UsageState = openAIAggregateAbsent
		} else if openAIValidAggregateUsage(result.Usage) {
			result.UsageState = openAIAggregatePresent
		}
	}
	return result, nil
}

func openAIBatchCounts(raw []byte) (int64, int64, int64, bool) {
	fields, err := batchapi.DecodeEvidenceProjection(raw, "request_counts")
	if err != nil {
		return 0, 0, 0, false
	}
	counts, err := batchapi.DecodeEvidenceProjection(fields["request_counts"], "total", "completed", "failed")
	if err != nil {
		return 0, 0, 0, false
	}
	total, totalOK := batchapi.TokenInteger(counts["total"], 0)
	completed, completedOK := batchapi.TokenInteger(counts["completed"], 0)
	failed, failedOK := batchapi.TokenInteger(counts["failed"], 0)
	if !totalOK || !completedOK || !failedOK || completed > total || failed != total-completed {
		return 0, 0, 0, false
	}
	return total, completed, failed, true
}
