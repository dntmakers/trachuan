package batchapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

func (s SDK) CreateRaw(ctx context.Context, requests []Request) ([]byte, int, error) {
	body := []byte(`{"requests":[`)
	for i := range requests {
		params, err := requests[i].Encode()
		if err != nil {
			return nil, 0, err
		}
		if i > 0 {
			body = append(body, ',')
		}
		id, err := json.Marshal(requests[i].CustomID)
		if err != nil {
			return nil, 0, fmt.Errorf("encode batch custom ID: %w", err)
		}
		body = append(body, `{"custom_id":`...)
		body = append(body, id...)
		body = append(body, `,"params":`...)
		body = append(body, params...)
		body = append(body, '}')
	}
	body = append(body, `]}`...)
	var raw []byte
	status := 0
	var discard []byte
	_, err := s.Client.Messages.Batches.New(ctx, anthropic.MessageBatchNewParams{}, option.WithRequestBody("application/json", body), option.WithResponseBodyInto(&discard), option.WithMiddleware(captureRawBody(&raw, &status)))
	return raw, status, err
}

func (s SDK) GetRaw(ctx context.Context, id string) ([]byte, int, error) {
	var raw []byte
	status := 0
	var discard []byte
	_, err := s.Client.Messages.Batches.Get(ctx, id, anthropic.MessageBatchGetParams{}, option.WithResponseBodyInto(&discard), option.WithMiddleware(captureRawBody(&raw, &status)))
	return raw, status, err
}

func captureRawBody(raw *[]byte, status *int) option.Middleware {
	return func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		response, err := next(req)
		if response == nil {
			return nil, err
		}
		*status = response.StatusCode
		*raw = []byte{}
		if response.Body == nil {
			return response, err
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		*raw = append(*raw, body...)
		response.Body = io.NopCloser(bytes.NewReader(body))
		if readErr != nil {
			return response, fmt.Errorf("read provider response body: %w", readErr)
		}
		return response, err
	}
}

func decodeBatchForSDK(raw []byte, requireStatus bool, expected string) (anthropic.MessageBatch, error) {
	var batch anthropic.MessageBatch
	fields, err := DecodeEvidenceObject(raw, "id", "created_at", "processing_status", "request_counts", "type", "error")
	if err != nil {
		return batch, fmt.Errorf("invalid provider batch: %w", err)
	}
	if value := fields["error"]; len(value) > 0 && string(value) != "null" {
		return batch, fmt.Errorf("provider response is an error envelope")
	}
	if value := fields["type"]; len(value) > 0 && string(value) != "null" {
		var kind string
		if value[0] != '"' || json.Unmarshal(value, &kind) != nil || kind != "message_batch" {
			return batch, fmt.Errorf("invalid provider batch type")
		}
	}
	var id, status string
	if len(fields["id"]) == 0 || fields["id"][0] != '"' || json.Unmarshal(fields["id"], &id) != nil || id == "" || (expected != "" && id != expected) {
		return batch, fmt.Errorf("invalid provider batch identity")
	}
	if value := fields["processing_status"]; len(value) > 0 && string(value) != "null" {
		if value[0] != '"' || json.Unmarshal(value, &status) != nil {
			return batch, fmt.Errorf("invalid provider batch status")
		}
	}
	if requireStatus && status != "ended" && status != "in_progress" && status != "canceling" {
		return batch, fmt.Errorf("invalid provider batch status")
	}
	if value := fields["created_at"]; len(value) > 0 && string(value) != "null" {
		var created time.Time
		if json.Unmarshal(value, &created) != nil {
			return batch, fmt.Errorf("invalid provider batch creation time")
		}
	}
	if value := fields["request_counts"]; value != nil {
		if _, err = DecodeEvidenceObject(value, "processing", "succeeded", "errored", "canceled", "expired"); err != nil {
			return batch, err
		}
	}
	if err = json.Unmarshal(raw, &batch); err != nil {
		return batch, fmt.Errorf("decode provider batch: %w", err)
	}
	return batch, nil
}

func guardProviderError(err error) error {
	if err == nil {
		return nil
	}
	var api *anthropic.Error
	if !errors.As(err, &api) {
		return err
	}
	fields, decodeErr := DecodeEvidenceObject([]byte(api.RawJSON()), "type", "error", "request_id")
	if decodeErr != nil {
		return fmt.Errorf("invalid provider error evidence")
	}
	if raw := fields["error"]; raw != nil {
		nested, nestedErr := DecodeEvidenceObject(raw, "type", "message", "details")
		if nestedErr != nil {
			return fmt.Errorf("invalid provider error evidence")
		}
		if details := nested["details"]; details != nil {
			if _, nestedErr = DecodeEvidenceObject(details, "error_code"); nestedErr != nil {
				return fmt.Errorf("invalid provider error evidence")
			}
		}
	}
	return err
}
