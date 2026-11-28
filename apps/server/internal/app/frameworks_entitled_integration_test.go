//go:build integration

package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/credential"
	"danny.vn/trachuan/apps/server/internal/mcpapi"
	"danny.vn/trachuan/apps/server/internal/weblogin"
	"danny.vn/trachuan/apps/server/internal/websession"

	"github.com/stretchr/testify/require"
)

func TestFrameworkEntitled_LiveStoreWithoutRelease(t *testing.T) {
	engine, _, _ := frameworkFixture(t, false, "")
	ctx := t.Context()
	auth := &frameworkTransportAuth{scopes: []string{"fw-alpha"}}
	check := frameworkEntitled(auth)
	require.True(t, check(ctx, 7))
	auth.entitlementErr = errors.New("invented failure")
	require.False(t, check(ctx, 7))
	auth.entitlementErr = nil
	require.True(t, check(ctx, 7))
	auth.scopes = nil
	require.False(t, check(ctx, 7))
	auth.scopes = []string{"fw-unknown"}
	require.True(t, check(ctx, 7))

	base := mcpapi.Config{FrameworkResolver: frameworkTransportLease{engine}, FrameworkAccess: frameworkCaller(auth), Limiter: frameworkTransportLimiter{}}
	mcp := func(cfg mcpapi.Config) http.Handler {
		verifier := &credential.Verifier{Store: auth, Audience: "https://example.test/mcp", Clock: func() time.Time { return time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC) }}
		return mcpapi.NoStore(credential.Middleware(verifier, mcpapi.New(cfg)))
	}
	web := func(cfg mcpapi.Config) http.Handler {
		sessions := &websession.Manager{Store: routeSessionStore{}, PublicURL: "http://localhost"}
		return newWebMux(webDeps{store: routeAccountStore{}, sessions: sessions, login: &weblogin.Handler{}, tools: cfg, loginLimit: newLoginLimiter(time.Now, nil), publicURL: sessions.PublicURL})
	}
	off := base
	on := base
	on.Frameworks = true
	dynamic := on
	dynamic.FrameworkEntitled = check
	mcpOff, mcpOn, mcpDynamic := mcp(off), mcp(on), mcp(dynamic)
	webOff, webOn, webDynamic := web(off), web(on), web(dynamic)
	mcpRequest := func(h http.Handler, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer trc_"+strings.Repeat("A", 43))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("Mcp-Protocol-Version", "2025-06-18")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	compare := func(mcpExpected, webExpected http.Handler) {
		t.Helper()
		for _, body := range []string{
			`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"guide","arguments":{}}}`,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mappings","arguments":{"corpus":"frameworks","handle":"fw:fw-alpha:1#A-1"}}}`,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"corpus":"frameworks","query":"quartz"}}}`,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read","arguments":{"corpus":"frameworks","items":[{"handle":"fw:fw-alpha:1#A-1"}]}}}`,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"corpus_status","arguments":{"corpus":"frameworks"}}}`,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"quality_gaps","arguments":{"corpus":"frameworks"}}}`,
		} {
			got, want := mcpRequest(mcpDynamic, body), mcpRequest(mcpExpected, body)
			require.Equal(t, want.Code, got.Code, body)
			require.Equal(t, want.Body.String(), got.Body.String(), body)
		}
		for _, route := range []struct {
			method, path string
			body         map[string]any
		}{
			{"GET", "/api/v1/frameworks", nil},
			{"GET", "/api/v1/frameworks/versions/fw:fw-alpha:1", nil},
			{"GET", "/api/v1/frameworks/controls/fw:fw-alpha:1%23A-1", nil},
			{"POST", "/api/v1/frameworks/search", map[string]any{"query": "quartz"}},
			{"GET", "/api/v1/frameworks/mappings?handle=fw%3Afw-alpha%3A1%23A-1", nil},
			{"GET", "/api/v1/frameworks/gaps", nil},
		} {
			webGot, _ := frameworkWebCall(t, webDynamic, route.method, route.path, nil, route.body)
			webWant, _ := frameworkWebCall(t, webExpected, route.method, route.path, nil, route.body)
			require.Equal(t, webWant.Code, webGot.Code, route.path)
			require.Equal(t, webWant.Body.String(), webGot.Body.String(), route.path)
		}
	}
	auth.scopes = []string{"fw-alpha"}
	compare(mcpOn, webOn)
	auth.entitlementErr = errors.New("invented failure")
	compare(mcpOff, webOff)
	auth.entitlementErr = nil
	compare(mcpOn, webOn)
	auth.scopes = nil
	compare(mcpOff, webOff)
}
