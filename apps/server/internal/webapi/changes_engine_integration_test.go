//go:build integration

package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/credential"
	"danny.vn/trachuan/apps/server/internal/limits"
	"danny.vn/trachuan/apps/server/internal/mcpapi"
	"danny.vn/trachuan/apps/server/internal/websession"
	"danny.vn/trachuan/internal/retrieval"
)

type countedChanges struct {
	*retrieval.Engine
	calls atomic.Int64
}

func (e *countedChanges) Changes(ctx context.Context, req retrieval.ChangesRequest) (retrieval.ChangesResult, error) {
	e.calls.Add(1)
	return e.Engine.Changes(ctx, req)
}

type countingChangesLimiter struct {
	calls   int
	classes []limits.Class
}

func (l *countingChangesLimiter) Allow(_ int64, class limits.Class) (bool, time.Duration) {
	l.calls++
	l.classes = append(l.classes, class)
	return true, 0
}

func TestChanges_RealHandlers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int
		sql     string
	}{
		{"available", 9, `INSERT INTO release_change_set VALUES (true,1,'vn','20991110_1','20991109',repeat('a',64),false,1,1,0,0,0,0,repeat('a',64)); INSERT INTO release_change(ordinal,kind,key,payload) VALUES (1,'document_added','vn:1','{"cause":"membership","key":"vn:1"}'),(2,'document_removed','vn:old','{"cause":"source","key":"vn:old"}')`},
		{"baseline", 9, `INSERT INTO release_change_set VALUES (true,1,'vn','20991110_1',NULL,NULL,true,0,0,0,0,0,0,repeat('a',64))`},
		{"unavailable", 8, `ALTER TABLE release DROP CONSTRAINT release_schema_version_check`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var extra []string
			if tc.sql != "" {
				extra = append(extra, tc.sql)
			}
			engine := &countedChanges{Engine: taxonomyIntegrationEngineVersion(t, retrieval.Recipe2(), taxonomyEmbedder{}, tc.version, extra...)}
			now := time.Date(2099, 11, 10, 0, 0, 0, 0, time.UTC)
			clock := func() time.Time { return now }
			limiter := &countingChangesLimiter{}
			resolver := &testResolver{engine: engine}
			cfg := mcpapi.Config{Resolver: resolver, Limiter: limiter, Clock: clock}
			sessions := &websession.Manager{Store: sessionStore{now: now}, Clock: clock, PublicURL: "https://example.test"}
			verifier := &credential.Verifier{Store: bearerStore{now: now}, Clock: clock, Audience: "https://example.test/mcp"}
			h := &harness{web: New(Config{Tools: cfg, Sessions: sessions}), mcp: credential.Middleware(verifier, mcpapi.New(cfg))}
			cursor := ""
			pages := 0
			for {
				args := `{"country":"vn","limit":1}`
				if cursor != "" {
					args = fmt.Sprintf(`{"country":"vn","limit":1,"cursor":%q}`, cursor)
				}
				web := webCall(h, "POST", "/api/v1/changes", args)
				mcp := mcpCall(t, h, "changes", args)
				pages++
				if web.Code != 200 || web.Header().Get("Cache-Control") != "no-store" || !bytes.Equal(web.Body.Bytes(), mcp) {
					t.Fatalf("web %d %s mcp %s", web.Code, web.Body.String(), mcp)
				}
				var result retrieval.ChangesResult
				if err := json.Unmarshal(mcp, &result); err != nil || result.State != tc.name {
					t.Fatalf("result %s: %v", mcp, err)
				}
				cursor = result.NextCursor
				if cursor == "" {
					break
				}
			}
			if limiter.calls != pages*2 {
				t.Fatalf("pages %d charges %d", pages, limiter.calls)
			}
			for _, class := range limiter.classes {
				if class != limits.Read {
					t.Fatal("wrong bucket")
				}
			}
			for _, mode := range []string{"missing", "expired", "revoked", "missing-csrf", "wrong-csrf", "bearer"} {
				t.Run(mode, func(t *testing.T) {
					before := engine.calls.Load()
					beforeCharges := limiter.calls
					manager := &websession.Manager{Store: changesSessionStore{sessionStore{now: now}, mode}, Clock: clock, PublicURL: "https://example.test"}
					handler := New(Config{Tools: cfg, Sessions: manager})
					request := webRequest("POST", "/api/v1/changes", `{"country":"vn"}`)
					switch mode {
					case "missing":
						request.Header.Del("Cookie")
					case "missing-csrf":
						request.Header.Del("X-CSRF-Token")
					case "wrong-csrf":
						request.Header.Set("X-CSRF-Token", "wrong")
					case "bearer":
						request.Header.Del("Cookie")
						request.Header.Set("Authorization", "Bearer tra_"+strings.Repeat("A", 43))
					}
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, request)
					if response.Code < 400 || engine.calls.Load() != before || limiter.calls != beforeCharges || response.Header().Get("Cache-Control") != "no-store" {
						t.Fatalf("status %d calls %d charges %d", response.Code, engine.calls.Load()-before, limiter.calls-beforeCharges)
					}
				})
			}
			for _, example := range []struct {
				args, code string
				status     int
			}{{`{"country":"vn","kinds":["relation"]}`, "invalid_request", 400}, {`{"country":"vn","release":"20990101"}`, "release_expired", 409}} {
				response := webCall(h, "POST", "/api/v1/changes", example.args)
				if response.Code != example.status || !strings.Contains(response.Body.String(), `"code":"`+example.code+`"`) {
					t.Fatalf("status %d %s", response.Code, response.Body.String())
				}
			}
		})
	}
}
