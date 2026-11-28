package mcpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/credential"
	"danny.vn/trachuan/apps/server/internal/limits"
)

func entitledMCPHandler(cfg Config) http.Handler {
	cfg.Limiter = limits.New(time.Now)
	v := &credential.Verifier{Store: authStore{}, Audience: "https://example.test/mcp", Clock: func() time.Time { return time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC) }}
	return NoStore(credential.Middleware(v, New(cfg)))
}

func entitledMCPRequest(h http.Handler, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer trc_"+strings.Repeat("A", 43))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

type countingAdmissionLimiter struct{ calls int }

func (l *countingAdmissionLimiter) Allow(int64, limits.Class) (bool, time.Duration) {
	l.calls++
	return true, 0
}

type unknownToolLimiter struct{ calls int }

func (l *unknownToolLimiter) Allow(int64, limits.Class) (bool, time.Duration) {
	l.calls++
	return false, time.Second
}

func TestEntitledMCP_UnknownToolDoesNotConsumeQuota(t *testing.T) {
	for _, tc := range []struct {
		name              string
		enabled, entitled bool
	}{
		{"off", false, false}, {"denied", true, false}, {"entitled", true, true}, {"true", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limiter := &unknownToolLimiter{}
			checks := 0
			cfg := Config{Frameworks: tc.enabled, Limiter: limiter}
			if tc.name != "true" {
				cfg.FrameworkEntitled = func(context.Context, int64) bool { checks++; return tc.entitled }
			}
			v := &credential.Verifier{Store: authStore{}, Audience: "https://example.test/mcp", Clock: func() time.Time { return time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC) }}
			h := NoStore(credential.Middleware(v, New(cfg)))
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"not-a-tool","arguments":{}}}`
			w := entitledMCPRequest(h, body)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"code":-32602`) || limiter.calls != 0 || checks != 0 {
				t.Fatalf("status=%d body=%s limiter calls=%d entitlement calls=%d", w.Code, w.Body.String(), limiter.calls, checks)
			}
		})
	}
}

func TestEntitledMCP_DisabledAdmissionKeepsToolsHidden(t *testing.T) {
	calls := 0
	checked := entitledMCPHandler(Config{FrameworkEntitled: func(context.Context, int64) bool { calls++; return true }})
	off := entitledMCPHandler(Config{})
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	got, want := entitledMCPRequest(checked, body), entitledMCPRequest(off, body)
	if calls != 1 || got.Code != want.Code || got.Body.String() != want.Body.String() {
		t.Fatalf("calls=%d got %d %s want %d %s", calls, got.Code, got.Body.String(), want.Code, want.Body.String())
	}
}

func TestEntitledMCP_ToolConsumesOneLimit(t *testing.T) {
	limiter := &countingAdmissionLimiter{}
	h := newHarness(t, func(cfg *Config) {
		cfg.Frameworks = true
		cfg.FrameworkEntitled = func(context.Context, int64) bool { return true }
		cfg.Limiter = limiter
	})
	_ = callTool(t, h, "guide", map[string]any{})
	if limiter.calls != 1 {
		t.Fatalf("tool consumed %d limits", limiter.calls)
	}
}

func TestEntitledMCP_InvalidStandardHeadersBeforeLimit(t *testing.T) {
	calls := 0
	cfg := Config{Frameworks: true, DiscoveryLimiter: &denyingLimiter{}, FrameworkEntitled: func(context.Context, int64) bool { calls++; return true }}
	v := &credential.Verifier{Store: authStore{}, Audience: "https://example.test/mcp", Clock: func() time.Time { return time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC) }}
	h := NoStore(credential.Middleware(v, New(cfg)))
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer trc_"+strings.Repeat("A", 43))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	r.Header.Set("Mcp-Method", "tools/call")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("status=%d entitlement calls=%d body=%s", w.Code, calls, w.Body.String())
	}
}

func TestEntitledMCP_MissingNameBeforeEntitlement(t *testing.T) {
	calls := 0
	h := entitledMCPHandler(Config{Frameworks: true, FrameworkEntitled: func(context.Context, int64) bool { calls++; return true }})
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mappings","arguments":{"corpus":"frameworks","handle":"fw:fw-alpha:1#A"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer trc_"+strings.Repeat("A", 43))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	r.Header.Set("Mcp-Method", "tools/call")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("status=%d entitlement calls=%d body=%s", w.Code, calls, w.Body.String())
	}
}

func TestEntitledMCP_RateLimitedMappingsMatchesEnabled(t *testing.T) {
	v := &credential.Verifier{Store: authStore{}, Audience: "https://example.test/mcp", Clock: func() time.Time { return time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC) }}
	makeHandler := func(check func(context.Context, int64) bool) http.Handler {
		return NoStore(credential.Middleware(v, New(Config{Frameworks: true, FrameworkEntitled: check, Limiter: &denyingLimiter{}})))
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mappings","arguments":{"corpus":"frameworks","handle":"fw:fw-alpha:1#A"}}}`
	got := entitledMCPRequest(makeHandler(func(context.Context, int64) bool { return true }), body)
	want := entitledMCPRequest(makeHandler(nil), body)
	if got.Code != want.Code || got.Body.String() != want.Body.String() {
		t.Fatalf("got %d %s want %d %s", got.Code, got.Body.String(), want.Code, want.Body.String())
	}
}

func TestEntitledMCP_RateLimitedListSkipsEntitlement(t *testing.T) {
	calls := 0
	cfg := Config{Frameworks: true, DiscoveryLimiter: &denyingLimiter{}, FrameworkEntitled: func(context.Context, int64) bool { calls++; return false }}
	v := &credential.Verifier{Store: authStore{}, Audience: "https://example.test/mcp", Clock: func() time.Time { return time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC) }}
	h := NoStore(credential.Middleware(v, New(cfg)))
	w := entitledMCPRequest(h, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	if w.Code != http.StatusTooManyRequests || calls != 0 {
		t.Fatalf("status=%d entitlement calls=%d", w.Code, calls)
	}
	cfg.Frameworks = false
	cfg.FrameworkEntitled = nil
	off := NoStore(credential.Middleware(v, New(cfg)))
	want := entitledMCPRequest(off, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	if w.Code != want.Code || w.Body.String() != want.Body.String() || w.Header().Get("Retry-After") != want.Header().Get("Retry-After") {
		t.Fatalf("rate-limited discovery differs: got %d %s headers=%v; want %d %s headers=%v", w.Code, w.Body.String(), w.Header(), want.Code, want.Body.String(), want.Header())
	}
}

func TestEntitledMCP_RateLimitPrecedesEntitlement(t *testing.T) {
	calls := 0
	h := newHarness(t, func(cfg *Config) {
		cfg.Frameworks = true
		cfg.FrameworkEntitled = func(context.Context, int64) bool { calls++; return false }
		cfg.Limiter = &denyingLimiter{}
	})
	calls = 0
	result := callTool(t, h, "search", map[string]any{"country": "vn", "query": "test"})
	wantError(t, result, "rate_limited")
	if calls != 0 {
		t.Fatalf("rate-limited request performed %d entitlement lookups", calls)
	}
}

func TestEntitledMCP_FutureProtocolMatchesEnabled(t *testing.T) {
	version := "2027-01-01"
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2027-01-01"}}}`
	request := func(h http.Handler) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer trc_"+strings.Repeat("A", 43))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("Mcp-Protocol-Version", version)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	on := entitledMCPHandler(Config{Frameworks: true})
	dynamic := entitledMCPHandler(Config{Frameworks: true, FrameworkEntitled: func(context.Context, int64) bool { return true }})
	got, want := request(dynamic), request(on)
	if got.Code != want.Code || got.Body.String() != want.Body.String() {
		t.Fatalf("future protocol differs: got %d %s, want %d %s", got.Code, got.Body.String(), want.Code, want.Body.String())
	}
}

func TestEntitledMCP_InvalidParametersSkipEntitlement(t *testing.T) {
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"cursor":123}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"guide","requestState":123}}`,
	} {
		calls := 0
		h := entitledMCPHandler(Config{Frameworks: true, FrameworkEntitled: func(context.Context, int64) bool { calls++; return false }})
		w := entitledMCPRequest(h, body)
		if calls != 0 {
			t.Fatalf("request=%s status=%d entitlement calls=%d", body, w.Code, calls)
		}
	}
}

func TestEntitledMCP_InvalidVersionMetadataSkipsEntitlement(t *testing.T) {
	calls := 0
	h := entitledMCPHandler(Config{Frameworks: true, FrameworkEntitled: func(context.Context, int64) bool { calls++; return false }})
	w := entitledMCPRequest(h, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2025-11-25"}}}`)
	if w.Code < 400 || calls != 0 {
		t.Fatalf("status=%d entitlement calls=%d", w.Code, calls)
	}
}

func TestEntitledMCP_InvalidHeadersSkipEntitlement(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"Content-Type", "text/plain"}, {"Accept", "text/plain"}, {"Mcp-Protocol-Version", "2020-01-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := entitledMCPHandler(Config{Frameworks: true, FrameworkEntitled: func(context.Context, int64) bool { calls++; return false }})
			r := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
			r.Header.Set("Authorization", "Bearer trc_"+strings.Repeat("A", 43))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			r.Header.Set("Mcp-Protocol-Version", "2025-06-18")
			r.Header.Set(tc.name, tc.value)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code < 400 || calls != 0 {
				t.Fatalf("status=%d entitlement calls=%d", w.Code, calls)
			}
		})
	}
}

func TestEntitledMCP_InvalidProtocolSkipsEntitlement(t *testing.T) {
	calls := 0
	h := entitledMCPHandler(Config{Frameworks: true, FrameworkEntitled: func(context.Context, int64) bool { calls++; return false }})
	w := entitledMCPRequest(h, `{invalid`)
	if w.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("status=%d entitlement calls=%d", w.Code, calls)
	}
}

func TestEntitledMCP_MatchesModesOnEveryRequest(t *testing.T) {
	allowed := false
	dynamic := entitledMCPHandler(Config{Frameworks: true, FrameworkEntitled: func(context.Context, int64) bool { return allowed }})
	off := entitledMCPHandler(Config{})
	on := entitledMCPHandler(Config{Frameworks: true})
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"guide","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mappings","arguments":{"corpus":"frameworks","handle":"fw:fw-alpha:1#A"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"corpus":"frameworks","query":"test"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read","arguments":{"corpus":"frameworks","items":[]}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"corpus_status","arguments":{"corpus":"frameworks"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"quality_gaps","arguments":{"corpus":"frameworks"}}}`,
	}
	for _, value := range []bool{false, true, false} {
		allowed = value
		expected := off
		if value {
			expected = on
		}
		for _, body := range requests {
			want := entitledMCPRequest(expected, body)
			got := entitledMCPRequest(dynamic, body)
			if got.Code != want.Code || got.Body.String() != want.Body.String() {
				t.Fatalf("allowed=%v request=%s got %d %s want %d %s", value, body, got.Code, got.Body.String(), want.Code, want.Body.String())
			}
		}
	}
}
