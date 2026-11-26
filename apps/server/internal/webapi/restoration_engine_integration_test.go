//go:build integration

package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/credential"
	"danny.vn/trachuan/apps/server/internal/limits"
	"danny.vn/trachuan/apps/server/internal/mcpapi"
	"danny.vn/trachuan/apps/server/internal/websession"
	"danny.vn/trachuan/internal/embedding"
	"danny.vn/trachuan/internal/retrieval"
)

type countedRestorationEmbedder struct{ calls atomic.Int64 }

func (*countedRestorationEmbedder) Recipe(ctx context.Context) (embedding.Recipe, error) {
	return (taxonomyEmbedder{}).Recipe(ctx)
}
func (e *countedRestorationEmbedder) Embed(ctx context.Context, text string) (embedding.Vector, error) {
	e.calls.Add(1)
	return (taxonomyEmbedder{}).Embed(ctx, text)
}

type countedRestorationEngine struct {
	*retrieval.Engine
	calls atomic.Int64
}

func (e *countedRestorationEngine) Search(ctx context.Context, req retrieval.Request) (retrieval.Response, error) {
	e.calls.Add(1)
	return e.Engine.Search(ctx, req)
}

func restorationHandlerFixture(t *testing.T, recipe retrieval.Recipe) (*harness, *mcpapi.Caches, *countedRestorationEngine, *countedRestorationEmbedder) {
	t.Helper()
	embedder := &countedRestorationEmbedder{}
	engine := &countedRestorationEngine{Engine: taxonomyIntegrationEngineWith(t, recipe, embedder)}
	now := time.Date(2099, 11, 10, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	resolver := &testResolver{engine: engine}
	caches := mcpapi.NewCaches(clock)
	caches.ActivateRelease("20991110_1", 1)
	cfg := mcpapi.Config{Resolver: resolver, Limiter: limits.New(clock), Clock: clock, Caches: caches}
	sessions := &websession.Manager{Store: sessionStore{now: now}, Clock: clock, PublicURL: "https://example.test"}
	verifier := &credential.Verifier{Store: bearerStore{now: now}, Clock: clock, Audience: "https://example.test/mcp"}
	return &harness{web: New(Config{Tools: cfg, Sessions: sessions}), mcp: credential.Middleware(verifier, mcpapi.New(cfg)), resolver: resolver}, caches, engine, embedder
}

func TestRestorationRealHandlers(t *testing.T) {
	t.Run("legacy", func(t *testing.T) {
		h, _, engine, embedder := restorationHandlerFixture(t, retrieval.Recipe3())
		args := `{"country":"vn","query":"ngan hang","limit":1}`
		for _, raw := range [][]byte{mcpCall(t, h, "search", args), webCall(h, "POST", "/api/v1/search", args).Body.Bytes()} {
			var result retrieval.SearchResult
			if err := json.Unmarshal(raw, &result); err != nil || !result.Abstain || result.Reason != "diacritics_required" {
				t.Fatalf("legacy result %s: %v", raw, err)
			}
		}
		if engine.calls.Load() != 0 || embedder.calls.Load() != 0 {
			t.Fatalf("legacy search %d embeds %d", engine.calls.Load(), embedder.calls.Load())
		}
	})
	t.Run("restoration", func(t *testing.T) {
		h, caches, engine, embedder := restorationHandlerFixture(t, retrieval.Recipe4())
		before := caches.Search.Stats()
		for _, transport := range []string{"mcp", "web"} {
			cursor := ""
			for page := 0; page < 2; page++ {
				args := fmt.Sprintf(`{"country":"vn","query":"ngan hang","limit":1,"cursor":%q}`, cursor)
				var raw []byte
				if transport == "mcp" {
					raw = mcpCall(t, h, "search", args)
				} else {
					response := webCall(h, "POST", "/api/v1/search", args)
					if response.Code != 200 {
						t.Fatalf("web status %d: %s", response.Code, response.Body.String())
					}
					raw = response.Body.Bytes()
				}
				var result retrieval.SearchResult
				if err := json.Unmarshal(raw, &result); err != nil || result.RestoredQuery != "ngân hàng" {
					t.Fatalf("%s page %d: %s %v", transport, page+1, raw, err)
				}
				cursor = result.NextCursor
				if page == 0 && cursor == "" {
					t.Fatal("missing continuation")
				}
			}
		}
		args := `{"country":"vn","query":"ngan hang","limit":1,"cursor":""}`
		mcpCall(t, h, "search", args)
		if engine.calls.Load() != 5 || embedder.calls.Load() != 5 {
			t.Fatalf("restoration search %d embeds %d", engine.calls.Load(), embedder.calls.Load())
		}
		if after := caches.Search.Stats(); after != before {
			t.Fatalf("restoration response cache changed: before %+v after %+v", before, after)
		}
		accented := `{"country":"vn","query":"ngân hàng","limit":1}`
		response := webCall(h, "POST", "/api/v1/search", accented)
		if response.Code != 200 {
			t.Fatalf("accented web: %d %s", response.Code, response.Body.String())
		}
		mcpCall(t, h, "search", accented)
		after := caches.Search.Stats()
		if engine.calls.Load() != 6 || embedder.calls.Load() != 6 || after.Misses != before.Misses+1 || after.Hits != before.Hits+1 || after.Bytes == 0 {
			t.Fatalf("accented search %d embeds %d cache %+v", engine.calls.Load(), embedder.calls.Load(), after)
		}
	})
}
