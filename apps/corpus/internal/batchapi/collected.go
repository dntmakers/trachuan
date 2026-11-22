package batchapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

func (s SDK) Results(ctx context.Context, id string) ([]Result, error) {
	var results []Result
	err := s.StreamResults(ctx, id, func(result Result) error {
		if err := verifyCollectedResult(result); err != nil {
			return err
		}
		results = append(results, result)
		return nil
	})
	return results, guardProviderError(err)
}

func (s SDK) List(ctx context.Context, after, before time.Time) ([]anthropic.MessageBatch, error) {
	var batches []anthropic.MessageBatch
	err := s.StreamPages(ctx, func(_ []anthropic.MessageBatch, raw string) error {
		page, decodeErr := rawListBatches(raw)
		if decodeErr != nil {
			return decodeErr
		}
		for _, batch := range page {
			if !batch.CreatedAt.Before(after) && !batch.CreatedAt.After(before) {
				batches = append(batches, batch)
			}
		}
		return nil
	})
	return batches, guardProviderError(err)
}

func rawListBatches(raw string) ([]anthropic.MessageBatch, error) {
	fields, err := DecodeEvidenceObject([]byte(raw), "data", "has_more", "first_id", "last_id")
	if err != nil {
		return nil, err
	}
	if string(fields["has_more"]) != "true" && string(fields["has_more"]) != "false" {
		return nil, fmt.Errorf("invalid provider list continuation")
	}
	var data []json.RawMessage
	if err = json.Unmarshal(fields["data"], &data); err != nil || data == nil {
		return nil, fmt.Errorf("invalid provider list data")
	}
	batches := make([]anthropic.MessageBatch, 0, len(data))
	for _, item := range data {
		obj, decodeErr := DecodeEvidenceObject(item, "id", "created_at", "processing_status", "request_counts")
		if decodeErr != nil {
			return nil, decodeErr
		}
		var batch anthropic.MessageBatch
		if len(obj["id"]) == 0 || obj["id"][0] != '"' || json.Unmarshal(obj["id"], &batch.ID) != nil || batch.ID == "" {
			return nil, fmt.Errorf("invalid listed batch identity")
		}
		if json.Unmarshal(obj["created_at"], &batch.CreatedAt) != nil || batch.CreatedAt.IsZero() {
			return nil, fmt.Errorf("invalid listed batch creation time")
		}
		var status string
		if len(obj["processing_status"]) == 0 || obj["processing_status"][0] != '"' || json.Unmarshal(obj["processing_status"], &status) != nil || status == "" {
			return nil, fmt.Errorf("invalid listed batch status")
		}
		batch.ProcessingStatus = anthropic.MessageBatchProcessingStatus(status)
		if counts := obj["request_counts"]; counts != nil {
			if _, decodeErr = DecodeEvidenceObject(counts, "processing", "succeeded", "errored", "canceled", "expired"); decodeErr != nil {
				return nil, decodeErr
			}
		}
		batches = append(batches, batch)
	}
	return batches, nil
}
