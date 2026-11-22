package batchapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
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

type SDK struct{ Client anthropic.Client }

func Parameters(r Request) anthropic.MessageBatchNewParamsRequest {
	p := r.Params
	return anthropic.MessageBatchNewParamsRequest{CustomID: r.CustomID, Params: anthropic.MessageBatchNewParamsRequestParams{Model: p.Model, Messages: p.Messages, MaxTokens: p.MaxTokens, OutputConfig: p.OutputConfig, Thinking: p.Thinking}}
}

func (s SDK) CountTokens(ctx context.Context, p anthropic.MessageNewParams) (int64, error) {
	v, err := s.Client.Messages.CountTokens(ctx, anthropic.MessageCountTokensParams{Model: p.Model, Messages: p.Messages, OutputConfig: p.OutputConfig, Thinking: p.Thinking})
	if err != nil {
		return 0, fmt.Errorf("count batch input: %w", err)
	}
	fields, err := DecodeObject([]byte(v.RawJSON()))
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
	body := []byte(`{"requests":[`)
	for i := range requests {
		params, err := requests[i].Encode()
		if err != nil {
			return anthropic.MessageBatch{}, err
		}
		if i > 0 {
			body = append(body, ',')
		}
		id, _ := json.Marshal(requests[i].CustomID)
		body = append(body, `{"custom_id":`...)
		body = append(body, id...)
		body = append(body, `,"params":`...)
		body = append(body, params...)
		body = append(body, '}')
	}
	body = append(body, `]}`...)
	b, err := s.Client.Messages.Batches.New(ctx, anthropic.MessageBatchNewParams{}, option.WithRequestBody("application/json", body))
	if err != nil {
		if b != nil {
			return *b, fmt.Errorf("create batch: %w", err)
		}
		return anthropic.MessageBatch{}, fmt.Errorf("create batch: %w", err)
	}
	return *b, nil
}

func (s SDK) Get(ctx context.Context, id string) (anthropic.MessageBatch, error) {
	b, err := s.Client.Messages.Batches.Get(ctx, id, anthropic.MessageBatchGetParams{})
	if err != nil {
		return anthropic.MessageBatch{}, fmt.Errorf("get batch: %w", err)
	}
	return *b, nil
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
	for page != nil && err == nil {
		if err = visit(page.Data, page.RawJSON()); err != nil {
			return err
		}
		if page.HasMore && (len(page.Data) == 0 || page.LastID == "") {
			return fmt.Errorf("provider list has no continuation cursor")
		}
		page, err = page.GetNextPage()
	}
	if err != nil {
		return fmt.Errorf("list batches: %w", err)
	}
	return nil
}

func (s SDK) Cancel(ctx context.Context, id string) error {
	_, err := s.Client.Messages.Batches.Cancel(ctx, id, anthropic.MessageBatchCancelParams{})
	if err != nil {
		return fmt.Errorf("cancel batch: %w", err)
	}
	return nil
}
