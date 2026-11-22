package batchapi

import (
	"context"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

func (s SDK) Results(ctx context.Context, id string) ([]Result, error) {
	var results []Result
	err := s.StreamResults(ctx, id, func(result Result) error {
		results = append(results, result)
		return nil
	})
	return results, err
}

func (s SDK) List(ctx context.Context, after, before time.Time) ([]anthropic.MessageBatch, error) {
	var batches []anthropic.MessageBatch
	err := s.StreamPages(ctx, func(page []anthropic.MessageBatch, _ string) error {
		for _, batch := range page {
			if !batch.CreatedAt.Before(after) && !batch.CreatedAt.After(before) {
				batches = append(batches, batch)
			}
		}
		return nil
	})
	return batches, err
}
