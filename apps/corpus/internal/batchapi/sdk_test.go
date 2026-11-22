package batchapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSDK_CountAndSubmitIdenticalAdaptiveRequest(t *testing.T) {
	calls := 0
	s := SDK{Client: anthropic.NewClient(option.WithoutEnvironmentDefaults(), option.WithAPIKey("synthetic"), option.WithMaxRetries(0), option.WithHTTPClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatal(err)
		}
		response := `{"input_tokens":42}`
		if strings.HasSuffix(r.URL.Path, "/batches") {
			item := body["requests"].([]any)[0].(map[string]any)
			if item["custom_id"] != "abcdef-000000" {
				t.Fatal("lost identity")
			}
			body = item["params"].(map[string]any)
			if body["max_tokens"] != float64(4096) {
				t.Fatal("lost output bound")
			}
			response = `{"id":"batch_synthetic","processing_status":"in_progress"}`
		}
		if body["thinking"].(map[string]any)["type"] != "adaptive" || body["output_config"].(map[string]any)["effort"] != "medium" {
			t.Fatal("lost thinking or effort")
		}
		if _, ok := body["temperature"]; ok {
			t.Fatal("sampling parameter")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
	})}))}
	p := anthropic.MessageNewParams{Model: "claude-haiku-5-5", MaxTokens: 4096, Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("synthetic legal text"))}, Thinking: anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}, OutputConfig: anthropic.OutputConfigParam{Effort: "medium", Format: anthropic.JSONOutputFormatParam{Schema: map[string]any{"type": "object"}}}}
	n, err := s.CountTokens(context.Background(), p)
	if err != nil || n != 42 {
		t.Fatalf("count %d error %v", n, err)
	}
	b, err := s.Create(context.Background(), []Request{{CustomID: "abcdef-000000", Params: p}})
	if err != nil || b.ID != "batch_synthetic" || calls != 2 {
		t.Fatalf("batch %s calls %d error %v", b.ID, calls, err)
	}
}

func TestSDK_CancelUsesProviderEndpoint(t *testing.T) {
	called := false
	s := SDK{Client: anthropic.NewClient(option.WithoutEnvironmentDefaults(), option.WithAPIKey("synthetic"), option.WithMaxRetries(0), option.WithHTTPClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/batches/batch_synthetic/cancel") {
			t.Fatal("wrong cancellation endpoint")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"batch_synthetic","processing_status":"canceling"}`))}, nil
	})}))}
	if err := s.Cancel(context.Background(), "batch_synthetic"); err != nil || !called {
		t.Fatalf("called %v error %v", called, err)
	}
}

func TestSDK_CreateSendsExactEncodedParams(t *testing.T) {
	request := Request{CustomID: "synthetic-000000", Params: anthropic.MessageNewParams{
		Model: "claude-haiku-5-5", MaxTokens: 128,
		Messages:     []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("a<b && c>d"))},
		OutputConfig: anthropic.OutputConfigParam{Format: anthropic.JSONOutputFormatParam{Schema: map[string]any{"properties": json.RawMessage(`{"z":{"type":"string"},"a":{"type":"string"}}`)}}},
	}}
	params, err := request.Encode()
	if err != nil {
		t.Fatal(err)
	}
	s := SDK{Client: anthropic.NewClient(option.WithoutEnvironmentDefaults(), option.WithAPIKey("synthetic"), option.WithMaxRetries(0), option.WithHTTPClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		var body struct {
			Requests []struct {
				Params json.RawMessage `json:"params"`
			} `json:"requests"`
		}
		if decodeErr := json.NewDecoder(r.Body).Decode(&body); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if r.URL.Path != Endpoint || len(body.Requests) != 1 || string(body.Requests[0].Params) != string(params) {
			t.Fatalf("endpoint %s, wire params %s, hashed params %s", r.URL.Path, body.Requests[0].Params, params)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"batch_synthetic"}`))}, nil
	})}))}
	if _, err = s.Create(context.Background(), []Request{request}); err != nil {
		t.Fatal(err)
	}
}

type interruptedBody struct{ *strings.Reader }

func (b interruptedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

func (interruptedBody) Close() error { return nil }

func TestSDK_StreamResultsYieldsLineBeforeTransportError(t *testing.T) {
	wire := `{"custom_id":"synthetic-000000","result":{"type":"expired"}}`
	s := SDK{Client: anthropic.NewClient(option.WithoutEnvironmentDefaults(), option.WithAPIKey("synthetic"), option.WithMaxRetries(0), option.WithHTTPClient(&http.Client{Transport: transportFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/x-jsonlines"}}, Body: interruptedBody{strings.NewReader(wire + "\n")}}, nil
	})}))}
	var results []Result
	err := s.StreamResults(context.Background(), "batch_synthetic", func(v Result) error {
		results = append(results, v)
		return nil
	})
	if err == nil || len(results) != 1 || results[0].CustomID != "synthetic-000000" || results[0].WireJSON != wire {
		t.Fatalf("decoded lines: %+v, stream error %v", results, err)
	}
}

func TestSDK_StreamPagesYieldsPageBeforeNextRequest(t *testing.T) {
	calls, captured := 0, 0
	wire := `{"data":[{"id":"batch_synthetic"}],"has_more":true,"first_id":"batch_synthetic","last_id":"batch_synthetic"}`
	s := SDK{Client: anthropic.NewClient(option.WithoutEnvironmentDefaults(), option.WithAPIKey("synthetic"), option.WithMaxRetries(0), option.WithHTTPClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
		}
		if captured != 1 || r.URL.Query().Get("after_id") != "batch_synthetic" {
			t.Fatalf("second page request before capture: %d, query %s", captured, r.URL.RawQuery)
		}
		return nil, io.ErrUnexpectedEOF
	})}))}
	err := s.StreamPages(context.Background(), func(batches []anthropic.MessageBatch, raw string) error {
		if len(batches) != 1 || batches[0].ID != "batch_synthetic" || raw != wire {
			t.Fatalf("batch page not preserved: %s", raw)
		}
		captured++
		return nil
	})
	if err == nil || calls != 2 || captured != 1 {
		t.Fatalf("calls %d, captured %d, error %v", calls, captured, err)
	}
}

func TestSDK_CountTokensRequiresPositiveRawInteger(t *testing.T) {
	for _, count := range []string{`true`, `1.5`, `"2"`, `0`, `-1`, `1e2`, `1,"input_tokens":2`} {
		t.Run(count, func(t *testing.T) {
			s := SDK{Client: anthropic.NewClient(option.WithoutEnvironmentDefaults(), option.WithAPIKey("synthetic"), option.WithMaxRetries(0), option.WithHTTPClient(&http.Client{Transport: transportFunc(func(_ *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"input_tokens":` + count + `}`))}, nil
			})}))}
			if n, err := s.CountTokens(context.Background(), anthropic.MessageNewParams{}); err == nil {
				t.Fatalf("invalid provider number became count %d", n)
			}
		})
	}
}
