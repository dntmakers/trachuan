package mcpapi

import (
	"testing"
	"time"
)

func TestSearchIssuedCacheKeys(t *testing.T) {
	h := newHarness(t, func(cfg *Config) {
		cfg.Caches = NewCaches(time.Now)
		cfg.Caches.ActivateRelease("20991111", 1)
	})
	for _, fields := range []map[string]any{
		{}, {"issued_from": "2024-01-01"}, {"issued_from": "2025-01-01"},
		{"issued_to": "2024-01-01"}, {"issued_to": "2025-01-01"},
	} {
		fields["country"] = "vn"
		fields["query"] = "ngân hàng"
		for range 2 {
			if result := callTool(t, h, "search", fields); result.IsError {
				t.Fatal(resultJSON(t, result))
			}
		}
	}
	if got := h.engine.calls.Load(); got != 10 {
		t.Fatalf("engine calls = %d, want 10", got)
	}
}

func TestSearchIssuedBounds(t *testing.T) {
	h := newHarness(t, nil)
	for _, args := range []map[string]any{
		{"issued_from": nil}, {"issued_from": ""}, {"issued_from": "2024-02-30"},
		{"issued_from": "2025-01-01", "issued_to": "2024-01-01"},
	} {
		args["country"] = "vn"
		args["query"] = "ngân hàng"
		wantError(t, callTool(t, h, "search", args), "invalid_request")
	}
	args := map[string]any{"country": "vn", "query": "ngân hàng", "issued_from": "2024-01-01", "issued_to": "2024-12-31"}
	out := callTool(t, h, "search", args)
	if out.IsError {
		t.Fatal(resultJSON(t, out))
	}
}
