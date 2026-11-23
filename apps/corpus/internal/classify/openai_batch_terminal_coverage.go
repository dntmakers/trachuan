package classify

import (
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

type openAIFileCapture struct {
	Complete bool
	Lines    [][]byte
}
type openAIBatchCapture struct {
	BatchID, OutputFileID, ErrorFileID string
	Output, Error                      *openAIFileCapture
}
type openAICoveredResult struct {
	CustomID string
	Raw      []byte
	Result   openAIResultLine
}

func verifyOpenAIBatchCoverage(batch openAIBatchTerminal, capture openAIBatchCapture, submitted []string, stage string) ([]openAICoveredResult, error) {
	fail := func() ([]openAICoveredResult, error) { return nil, fmt.Errorf("incomplete batch result evidence") }
	if stage != "document" && stage != "article" {
		return fail()
	}
	if capture.BatchID != batch.Identity.BatchID || capture.OutputFileID != batch.OutputFileID || capture.ErrorFileID != batch.ErrorFileID {
		return fail()
	}
	if !batch.CountsUsable || batch.UsageState == openAIAggregateInvalid || len(submitted) == 0 || int64(len(submitted)) != batch.Total || batch.Completed > batch.Total || batch.Failed != batch.Total-batch.Completed {
		return fail()
	}
	remaining := make(map[string]bool, len(submitted))
	for _, id := range submitted {
		if id == "" || remaining[id] {
			return fail()
		}
		remaining[id] = true
	}
	var results []openAICoveredResult
	for _, part := range []struct {
		id    string
		file  *openAIFileCapture
		count int64
	}{{batch.OutputFileID, capture.Output, batch.Completed}, {batch.ErrorFileID, capture.Error, batch.Failed}} {
		if part.id == "" {
			if part.count != 0 || part.file != nil {
				return fail()
			}
			continue
		}
		if part.file == nil || !part.file.Complete || int64(len(part.file.Lines)) != part.count {
			return fail()
		}
		for _, raw := range part.file.Lines {
			fields, err := batchapi.DecodeEvidenceProjection(raw, "custom_id")
			if err != nil {
				return fail()
			}
			id, err := evidenceString(fields["custom_id"])
			if err != nil || !remaining[id] {
				return fail()
			}
			parsed, err := parseOpenAIResultLine(raw, id, stage)
			if err != nil {
				return fail()
			}
			delete(remaining, id)
			results = append(results, openAICoveredResult{CustomID: id, Raw: append([]byte(nil), raw...), Result: parsed})
		}
	}
	if len(remaining) != 0 {
		return fail()
	}
	return results, nil
}
