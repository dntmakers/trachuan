package batchapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

const Provider = "anthropic"
const Endpoint = "/v1/messages/batches"

type Request struct {
	CustomID string
	Params   anthropic.MessageNewParams
	encoded  []byte
}

type Result struct {
	CustomID, Type       string
	Message              anthropic.Message
	RequestID, ErrorJSON string
	RawJSON              string
	WireJSON             string
}

type SDK struct {
	Client      anthropic.Client
	Fingerprint string
}

func (s SDK) AccountFingerprint() string { return s.Fingerprint }

func Parameters(r Request) anthropic.MessageBatchNewParamsRequest {
	p := r.Params
	return anthropic.MessageBatchNewParamsRequest{CustomID: r.CustomID, Params: anthropic.MessageBatchNewParamsRequestParams{Model: p.Model, Messages: p.Messages, System: p.System, MaxTokens: p.MaxTokens, OutputConfig: p.OutputConfig, Thinking: p.Thinking}}
}

func (s SDK) CountTokens(ctx context.Context, p anthropic.MessageNewParams) (int64, error) {
	v, err := s.Client.Messages.CountTokens(ctx, anthropic.MessageCountTokensParams{Model: p.Model, Messages: p.Messages, System: anthropic.MessageCountTokensParamsSystemUnion{OfTextBlockArray: p.System}, OutputConfig: p.OutputConfig, Thinking: p.Thinking})
	if err != nil {
		return 0, fmt.Errorf("count batch input: %w", err)
	}
	fields, err := DecodeEvidenceObject([]byte(v.RawJSON()), "input_tokens")
	if err != nil {
		return 0, fmt.Errorf("decode batch input count: %w", err)
	}
	n, ok := TokenInteger(fields["input_tokens"], 1)
	if !ok {
		return 0, fmt.Errorf("provider input_tokens is not a positive JSON integer")
	}
	return n, nil
}

func (r *Request) Encode() ([]byte, error) {
	if r.encoded == nil {
		b, err := json.Marshal(Parameters(*r).Params)
		if err != nil {
			return nil, fmt.Errorf("encode batch params: %w", err)
		}
		r.encoded = b
	}
	return r.encoded, nil
}

func (s SDK) Create(ctx context.Context, requests []Request) (anthropic.MessageBatch, error) {
	raw, status, err := s.CreateRaw(ctx, requests)
	if err != nil {
		return anthropic.MessageBatch{}, fmt.Errorf("create batch: %w", guardProviderError(err))
	}
	if status < 200 || status >= 300 {
		return anthropic.MessageBatch{}, fmt.Errorf("provider create response was not successful")
	}
	return decodeBatchForSDK(raw, false, "")
}

func (s SDK) Get(ctx context.Context, id string) (anthropic.MessageBatch, error) {
	raw, status, err := s.GetRaw(ctx, id)
	if err != nil {
		return anthropic.MessageBatch{}, fmt.Errorf("get batch: %w", guardProviderError(err))
	}
	if status < 200 || status >= 300 {
		return anthropic.MessageBatch{}, fmt.Errorf("provider get response was not successful")
	}
	return decodeBatchForSDK(raw, true, id)
}

func (s SDK) StreamResults(ctx context.Context, id string, visit func(Result) error) error {
	stream := s.Client.Messages.Batches.ResultsStreaming(ctx, id, anthropic.MessageBatchResultsParams{})
	defer func() { _ = stream.Close() }()
	for stream.Next() {
		v := stream.Current()
		result := Result{CustomID: v.CustomID, Type: v.Result.Type, Message: v.Result.Message, RequestID: v.Result.Error.RequestID, ErrorJSON: v.Result.Error.RawJSON(), RawJSON: v.Result.RawJSON(), WireJSON: v.RawJSON()}
		if err := visit(result); err != nil {
			return err
		}
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("read batch results: %w", err)
	}
	return nil
}

func (s SDK) StreamPages(ctx context.Context, visit func([]anthropic.MessageBatch, string) error) error {
	page, err := s.Client.Messages.Batches.List(ctx, anthropic.MessageBatchListParams{})
	seen := map[string]bool{}
	for page != nil && err == nil {
		raw := page.RawJSON()
		if err = visit(page.Data, raw); err != nil {
			return err
		}
		fields, decodeErr := DecodeEvidenceObject([]byte(raw), "data", "has_more", "first_id", "last_id")
		if decodeErr != nil {
			return fmt.Errorf("invalid provider list page: %w", decodeErr)
		}
		var data []json.RawMessage
		if json.Unmarshal(fields["data"], &data) != nil || data == nil {
			return fmt.Errorf("invalid provider list data")
		}
		switch string(fields["has_more"]) {
		case "false":
			return nil
		case "true":
		default:
			return fmt.Errorf("invalid provider list continuation")
		}
		var cursor string
		if len(data) == 0 || len(fields["last_id"]) == 0 || fields["last_id"][0] != '"' || json.Unmarshal(fields["last_id"], &cursor) != nil || cursor == "" || seen[cursor] {
			return fmt.Errorf("provider list has no advancing cursor")
		}
		if !page.HasMore || page.LastID != cursor {
			return fmt.Errorf("provider list cursor differs from raw page")
		}
		seen[cursor] = true
		page, err = page.GetNextPage()
		if page == nil && err == nil {
			return fmt.Errorf("provider list ended before final page")
		}
	}
	if err != nil {
		return fmt.Errorf("list batches: %w", err)
	}
	return fmt.Errorf("provider list returned no page")
}

func (s SDK) Cancel(ctx context.Context, id string) error {
	_, err := s.Client.Messages.Batches.Cancel(ctx, id, anthropic.MessageBatchCancelParams{})
	if err != nil {
		return fmt.Errorf("cancel batch: %w", err)
	}
	return nil
}
