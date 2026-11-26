//go:build integration

package mcpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/credential"
	"danny.vn/trachuan/apps/server/internal/limits"
	"danny.vn/trachuan/apps/server/internal/store"
	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/embedding"
	"danny.vn/trachuan/internal/releasechanges"
	"danny.vn/trachuan/internal/retrieval"
	"danny.vn/trachuan/internal/retrieval/lexical"
	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5/pgxpool"
)

type changesTexts struct{ done bool }

func (s *changesTexts) Next(context.Context) (string, bool, error) {
	if s.done {
		return "", false, nil
	}
	s.done = true
	return "văn bản", true, nil
}

type changesEmbedder struct{}

func (changesEmbedder) Recipe(context.Context) (embedding.Recipe, error) {
	return embedding.QueryRecipe1(), nil
}
func (changesEmbedder) Embed(context.Context, string) (embedding.Vector, error) {
	return embedding.Vector{}, nil
}

type countedChangesEngine struct {
	*retrieval.Engine
	calls atomic.Int64
}

func (e *countedChangesEngine) Changes(ctx context.Context, req retrieval.ChangesRequest) (retrieval.ChangesResult, error) {
	e.calls.Add(1)
	return e.Engine.Changes(ctx, req)
}

type changesResolver struct{ engine *countedChangesEngine }

func (r changesResolver) Resolve(_ context.Context, _, _ string) (Lease, error) {
	return Lease{Engine: r.engine, Release: "20991110_1", Done: func() {}}, nil
}

func changesFixture(t *testing.T, version int, baseline bool) (*harness, *changesLimiter, *countedChangesEngine) {
	t.Helper()
	ctx := context.Background()
	db, err := pgxpool.New(ctx, testpg.NewDatabase(t, "vector", "ltree"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	q, d := embedding.QueryRecipe1(), embedding.DocumentRecipe1()
	if err = corpusdb.Apply(ctx, db, q.Dimensions); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	_, err = db.Exec(ctx, `INSERT INTO document(id,country,key,number,number_norm,title,doc_type,issuer,class,source,validity_conflict,eff_from,raw_issue_date,raw_eff_from,raw_eff_to,status_code,status_name,status_class,status_basis,is_current,is_consolidated,content_state,fetch_sha256,text_source,text_complete) VALUES(1,'vn','vn:1','n','n','title','law','issuer','primary','vbpl',false,'2020-01-01','','','','','','in_force','source_status',true,false,'indexed',$1,'source_tree',true)`, hash)
	if err != nil {
		t.Fatal(err)
	}
	if version == 9 {
		tx, beginErr := db.Begin(ctx)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		prev := "20991109"
		key, old := "vn:1", "vn:old"
		header := releasechanges.Header{Version: 1, Country: "vn", Release: "20991110_1", Baseline: baseline}
		var rows []releasechanges.Row
		if !baseline {
			header.Predecessor = &prev
			header.PredecessorIdentityHash = &hash
			rows = []releasechanges.Row{
				{Ordinal: 1, Kind: "document_added", Key: &key, Payload: `{"cause":"membership","key":"vn:1"}`},
				{Ordinal: 2, Kind: "document_removed", Key: &old, Payload: `{"cause":"source","key":"vn:old"}`},
				{Ordinal: 3, Kind: "document_validity", Key: &key, Payload: `{"eff_to":{"after":"2024-01-01","before":null},"key":"vn:1"}`},
				{Ordinal: 4, Kind: "provision_validity", Key: &key, Payload: `{"anchor":"p1","change":"removed","key":"vn:1","validity":{"eff_from":null,"eff_to":null,"method":"source","status_class":"in_force"}}`},
				{Ordinal: 5, Kind: "provision_text", Key: &key, Payload: `{"ambiguous":false,"anchor":"p1","change":"changed","key":"vn:1","provenance":false}`},
				{Ordinal: 6, Kind: "document_key", Key: &key, Key2: &old, Payload: `{"before_key":"vn:old","key":"vn:1"}`},
			}
		}
		if err = corpusdb.WriteReleaseChanges(ctx, tx, header, rows); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := lexical.Train(ctx, &changesTexts{}, lexical.Recipe1())
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2099, 11, 10, 0, 0, 0, 0, time.UTC) }
	rel := retrieval.Release{Country: "vn", Rel: "20991110_1", SchemaVersion: version, RetrievalRecipe: retrieval.Recipe2(), LexicalRecipe: lexical.Recipe1(), ChunkRecipe: json.RawMessage(`{}`), EvaluationSummary: json.RawMessage(`{}`), SourceManifestHash: hash, BuildInputHash: hash, BuildRecipeHash: hash, BuiltAt: now(), LexicalStats: stats.Summary(lexical.Recipe1(), nil), DenseRecipe: retrieval.DenseRecipe{Version: 1, Model: q.Model, Dimensions: q.Dimensions, Document: d, DocumentHash: d.Hash(), Query: q, QueryHash: q.Hash()}}
	engine, err := retrieval.OpenDraft(ctx, db, changesEmbedder{}, rel, retrieval.Options{Now: now, Profile: "evaluation"})
	if err != nil {
		t.Fatal(err)
	}
	limiter := &changesLimiter{}
	counted := &countedChangesEngine{Engine: engine}
	h := newHarness(t, func(c *Config) { c.Resolver = changesResolver{counted}; c.Limiter = limiter })
	return h, limiter, counted
}

func TestChanges_RealEngineCredentials(t *testing.T) {
	_, _, engine := changesFixture(t, 9, false)
	now := time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		status int
	}{{"missing", 401}, {"expired", 401}, {"revoked", 401}, {"scope", 403}} {
		t.Run(tc.name, func(t *testing.T) {
			row := store.GetTokenRow{Kind: "access", AccountID: 7, GrantID: 9, Resource: "https://example.test/mcp", Scope: "mcp:read", ExpiresAt: now.Add(time.Hour), FamilyExpiresAt: now.Add(time.Hour)}
			switch tc.name {
			case "expired":
				row.ExpiresAt = now
			case "revoked":
				row.RevokedAt = &now
			case "scope":
				row.Scope = "other"
			}
			verifier := &credential.Verifier{Store: changesAuthStore{row}, Audience: "https://example.test/mcp", Clock: func() time.Time { return now }}
			limiter := &changesLimiter{}
			handler := credential.Middleware(verifier, New(Config{Resolver: changesResolver{engine}, Limiter: limiter}))
			request := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"changes","arguments":{"country":"vn"}}}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			if tc.name != "missing" {
				request.Header.Set("Authorization", "Bearer tra_"+strings.Repeat("A", 43))
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status || engine.calls.Load() != 0 || limiter.calls != 0 {
				t.Fatalf("status %d engine calls %d limiter calls %d", response.Code, engine.calls.Load(), limiter.calls)
			}
		})
	}
}

func TestChanges_RealEngine(t *testing.T) {
	for _, tc := range []struct {
		name     string
		version  int
		baseline bool
		state    string
	}{{"available", 9, false, "available"}, {"baseline", 9, true, "baseline"}, {"unavailable", 8, false, "unavailable"}} {
		t.Run(tc.name, func(t *testing.T) {
			h, limiter, _ := changesFixture(t, tc.version, tc.baseline)
			pages := 0
			args := map[string]any{"country": "vn", "limit": 1, "kinds": []string{"document_added", "document_removed", "document_validity", "provision_validity", "provision_text", "document_key"}}
			var items []map[string]any
			for {
				result := callTool(t, h, "changes", args)
				pages++
				out := resultJSON(t, result)
				if result.IsError || out["state"] != tc.state {
					t.Fatalf("result: %+v", out)
				}
				page, ok := out["items"].([]any)
				if !ok {
					t.Fatalf("missing items: %+v", out)
				}
				if tc.state == "available" {
					counts, ok := out["counts"].(map[string]any)
					if !ok || len(counts) != 6 || counts["document_removed"] != float64(1) || out["previous_release"] != "20991109" {
						t.Fatalf("comparison metadata: %+v", out)
					}
				}
				if tc.state != "available" && (len(page) != 0 || out["counts"] != nil) {
					t.Fatalf("empty state: %+v", out)
				}
				for _, item := range page {
					items = append(items, item.(map[string]any))
				}
				cursor, _ := out["next_cursor"].(string)
				if cursor == "" {
					break
				}
				args["cursor"] = cursor
			}
			if limiter.calls != pages {
				t.Fatalf("pages %d, charges %d", pages, limiter.calls)
			}
			for _, class := range limiter.classes {
				if class != limits.Read {
					t.Fatal("wrong bucket")
				}
			}
			if tc.state == "available" {
				if len(items) != 6 || items[0]["handle"] != "vn:1" || items[1]["kind"] != "document_removed" || items[1]["handle"] != nil {
					t.Fatalf("items: %+v", items)
				}
				pair := items[2]["eff_to"].(map[string]any)
				if v, ok := pair["before"]; !ok || v != nil {
					t.Fatalf("nullable pair: %+v", pair)
				}
			}
			wantError(t, callTool(t, h, "changes", map[string]any{"country": "vn", "kinds": []string{"relation"}}), "invalid_request")
			wantError(t, callTool(t, h, "changes", map[string]any{"country": "vn", "release": "20990101"}), "release_expired")
		})
	}
}
