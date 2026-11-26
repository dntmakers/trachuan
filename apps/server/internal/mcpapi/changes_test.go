package mcpapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/limits"
	"danny.vn/trachuan/internal/retrieval"
)

type changesLimiter struct {
	calls   int
	classes []limits.Class
}

func (l *changesLimiter) Allow(_ int64, c limits.Class) (bool, time.Duration) {
	l.calls++
	l.classes = append(l.classes, c)
	return true, 0
}

func TestChanges_ValidationAndBucket(t *testing.T) {
	limiter := &changesLimiter{}
	h := newHarness(t, func(c *Config) { c.Limiter = limiter })
	for _, raw := range []string{`{"country":"vn","kinds":["relation"]}`, `{"country":"vn","kinds":[]}`, `{"country":"vn","limit":null}`, `{"country":"vn","extra":1}`, `{"country":"vn","document":"vn:"}`} {
		wantError(t, callTool(t, h, "changes", json.RawMessage(raw)), "invalid_request")
	}
	if h.engine.calls.Load() != 0 {
		t.Fatal("invalid input reached engine")
	}
	for i := 0; i < 2; i++ {
		result := callTool(t, h, "changes", map[string]any{"country": "vn"})
		if result.IsError || resultJSON(t, result)["state"] != "baseline" {
			t.Fatal(result)
		}
	}
	if h.engine.calls.Load() != 2 || limiter.calls != 7 {
		t.Fatalf("engine calls %d limiter calls %d", h.engine.calls.Load(), limiter.calls)
	}
	for _, class := range limiter.classes {
		if class != limits.Read {
			t.Fatal("wrong bucket")
		}
	}
	h.engine.err = retrieval.ErrReleaseExpired
	wantError(t, callTool(t, h, "changes", map[string]any{"country": "vn"}), "release_expired")
	h.engine.err = retrieval.ErrUnavailable
	wantError(t, callTool(t, h, "changes", map[string]any{"country": "vn"}), "unavailable")
}

func TestChanges_Listed(t *testing.T) {
	h := newHarness(t, nil)
	result, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range result.Tools {
		if tool.Name == "changes" {
			if tool.InputSchema == nil || tool.OutputSchema == nil {
				t.Fatal("missing schema")
			}
			return
		}
	}
	t.Fatal("changes tool missing")
}
