package classify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

func storeResult(t *testing.T, id, body string) batchapi.Result {
	t.Helper()
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatal(err)
	}
	return batchapi.Result{CustomID: id, Type: "succeeded", Message: msg}
}

func TestResultStore_PricesCacheUsage(t *testing.T) {
	r, ledger, f, o := screenFixture(t)
	f.beforeCreate = func(req []batchapi.Request) {
		f.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}"}],"usage":{"input_tokens":10,"cache_creation_input_tokens":60,"cache_read_input_tokens":30,"output_tokens":10}}`)}
	}
	summary, err := r.Screen(context.Background(), o, documents(), families())
	if err != nil || summary.ReserveMicroUSD != 1031 || !ledger.rows[0].Settled || ledger.rows[0].CostMicroUSD != 7 {
		t.Fatalf("summary %+v, ledger %+v, error %v", summary, ledger.rows, err)
	}
}

func TestResultStore_RequestKeyUsesBatchParams(t *testing.T) {
	r, ledger, f, o := screenFixture(t)
	before := f.beforeCreate
	f.beforeCreate = func(req []batchapi.Request) {
		before(req)
		p, err := json.Marshal(batchapi.Parameters(req[0]).Params)
		if err != nil {
			t.Fatal(err)
		}
		want := digest(append([]byte("anthropic\x00/v1/messages/batches\x00"), p...))
		if got := ledger.rows[0].Items[0].RequestHash; got != want {
			t.Fatalf("request key = %s, want %s", got, want)
		}
	}
	if _, err := r.Screen(context.Background(), o, documents(), families()); err != nil {
		t.Fatal(err)
	}
}

func TestResultStore_ExpiredAndCanceledAreFailedAtZero(t *testing.T) {
	for _, state := range []string{"expired", "canceled"} {
		t.Run(state, func(t *testing.T) {
			r, ledger, f, o := screenFixture(t)
			f.beforeCreate = func(req []batchapi.Request) {
				f.results = []batchapi.Result{{CustomID: req[0].CustomID, Type: state}}
			}
			if _, err := r.Screen(context.Background(), o, documents(), families()); err != nil {
				t.Fatal(err)
			}
			s := ledger.rows[0]
			b, err := os.ReadFile(filepath.Join(r.IntentDir, "results", s.Items[0].RequestHash, s.ID+"-000000.json"))
			if err != nil || !s.Settled || s.CostMicroUSD != 0 || !strings.Contains(string(b), `"outcome":"failed"`) {
				t.Fatalf("attempt %s, error %v; settled=%v cost=%d", b, err, s.Settled, s.CostMicroUSD)
			}
		})
	}
}

func TestResultStore_ReplayAcrossRebuiltLedger(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	if _, err := r.Screen(ctx, o, documents(), families()); err != nil {
		t.Fatal(err)
	}
	s := ledger.rows[0]
	if _, err := os.Stat(filepath.Join(r.IntentDir, "submissions", s.ID+".settled")); err != nil {
		t.Fatal(err)
	}
	ledger.rows = nil
	if err := r.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ledger.rows) != 1 || !ledger.rows[0].Settled || ledger.rows[0].CostMicroUSD != s.CostMicroUSD {
		t.Fatalf("rebuilt ledger: %+v", ledger.rows)
	}
	o.RunID++
	o.Release = "20990102"
	o.Out = filepath.Join(t.TempDir(), "replay")
	counts := f.counts
	r.Client = nil
	res, err := r.Screen(ctx, o, documents(), families())
	b, readErr := os.ReadFile(filepath.Join(o.Out, "families.json"))
	if err != nil || res.Requests != 0 || res.ReserveMicroUSD != 0 || len(ledger.rows) != 1 || f.creates != 1 || f.counts != counts || readErr != nil || !strings.Contains(string(b), `"retired"`) {
		t.Fatalf("replay %+v, error %v; creates=%d counts=%d artifact=%s read=%v", res, err, f.creates, f.counts, b, readErr)
	}
}

func TestResultStore_ThirdFailureStopsResubmission(t *testing.T) {
	r, ledger, f, o := screenFixture(t)
	f.beforeCreate = func(req []batchapi.Request) {
		f.results = []batchapi.Result{{CustomID: req[0].CustomID, Type: "expired"}}
	}
	for i := 0; i < 4; i++ {
		o.RunID = int64(i + 1)
		o.Out = filepath.Join(t.TempDir(), "families")
		if _, err := r.Screen(context.Background(), o, documents(), families()); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(o.Out, "families.json"))
	if len(ledger.rows) != 3 || f.creates != 3 || err != nil || !strings.Contains(string(b), `"invalid_response"`) {
		t.Fatalf("rows=%d creates=%d artifact=%s error=%v", len(ledger.rows), f.creates, b, err)
	}
}

func TestResultStore_IncompleteBatchLocksAcrossReleases(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.beforeCreate = nil
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil || len(ledger.rows) != 1 || ledger.rows[0].Settled {
		t.Fatalf("incomplete results released reserve: %+v, error %v", ledger.rows, err)
	}
	o.RunID++
	o.Release = "20990102"
	o.Out = filepath.Join(t.TempDir(), "families")
	if res, err := r.Screen(ctx, o, documents(), families()); err != nil || res.HeldKeys != 1 || f.creates != 1 {
		t.Fatalf("in-flight hold %+v: creates=%d, error %v", res, f.creates, err)
	}
}

func TestResultStore_ReplaysPartialArticleResponseThroughValidation(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o, familyArtifact := runArticlesFixture(t)
	input := articleInputs(2, "same-chapter")
	f.beforeCreate = func(req []batchapi.Request) {
		f.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"dieu-1\":{\"codes\":[{\"code\":\"retired\",\"confidence\":0.9}]},\"foreign\":{\"codes\":[]}}"}],"usage":{"input_tokens":100,"output_tokens":10}}`)}
	}
	if _, err := r.Articles(ctx, o, "labels", input, familyArtifact, articleNodes()); err != nil {
		t.Fatal(err)
	}
	s := ledger.rows[0]
	attempt, _, found, err := r.readAttempt(s, 0)
	if err != nil || !found || attempt.Outcome != "final" {
		t.Fatalf("attempt %+v, found %v, error %v", attempt, found, err)
	}
	b, err := os.ReadFile(filepath.Join(o.Out, "articles.json"))
	if err != nil || !strings.Contains(string(b), `"dropped_anchors": 1`) || !strings.Contains(string(b), `"invalid_response"`) || !strings.Contains(string(b), `"retired"`) {
		t.Fatalf("fresh artifact %s, error %v", b, err)
	}
	o.RunID++
	o.Out = filepath.Join(t.TempDir(), "replay")
	res, err := r.Articles(ctx, o, "labels", input, familyArtifact, articleNodes())
	replayed, readErr := os.ReadFile(filepath.Join(o.Out, "articles.json"))
	if err != nil || readErr != nil || res.Requests != 0 || f.creates != 1 || string(b) != string(replayed) {
		t.Fatalf("replay %+v, error %v, creates %d, artifact %s, read error %v", res, err, f.creates, replayed, readErr)
	}
}

func TestResultStore_UnpricedResultRetainsReserveAndEvidence(t *testing.T) {
	r, ledger, f, o := screenFixture(t)
	f.beforeCreate = func(req []batchapi.Request) {
		f.results = []batchapi.Result{{CustomID: req[0].CustomID, Type: "errored", ErrorJSON: `{"type":"error","error":{"type":"overloaded_error","message":"synthetic"}}`}}
	}
	if _, err := r.Screen(context.Background(), o, documents(), families()); err == nil || ledger.rows[0].Settled {
		t.Fatalf("unpriced result released reserve: %+v, error %v", ledger.rows, err)
	}
	s := ledger.rows[0]
	a, _, found, err := r.readAttempt(s, 0)
	if err != nil || !found || a.Outcome != "failed" || a.Cost != nil || !strings.Contains(a.Raw, "overloaded_error") {
		t.Fatalf("attempt %+v, found %v, error %v", a, found, err)
	}
	if _, err = os.Stat(r.settlementPath(s)); !os.IsNotExist(err) {
		t.Fatalf("unexpected settlement: %v", err)
	}
}

func TestResultStore_RebuildImportsHistoricalPilotCost(t *testing.T) {
	ctx := context.Background()
	r, ledger, _, o := screenFixture(t)
	o.Recipe.PromptVersion = 1
	s := Submission{ID: strings.Repeat("a", 32), RunID: o.RunID, Country: o.Country, Release: o.Release, Recipe: o.Recipe, State: "preparing", CreatedAt: r.Now(), Budget: o.Budget, BudgetMicroUSD: o.BudgetMicroUSD, ReserveMicroUSD: 1029, Items: []Item{{Key: documents()[0].Key, RequestHash: strings.Repeat("b", 64), InputTokens: 100}}}
	s.ItemsHash = itemsHash(s.Items)
	if err := r.writeIntent(s); err != nil {
		t.Fatal(err)
	}
	if err := r.recordBatch(&s, "batch_pilot"); err != nil {
		t.Fatal(err)
	}
	s.State, s.CostMicroUSD, s.Settled = "completed", 17, true
	s.Results = map[string]Outcome{s.Items[0].Key: {Outcome: "no_family", Codes: []string{}}}
	ledger.rows = []Submission{s}
	if err := r.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	ledger.rows = nil
	if err := r.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	got := ledger.rows[0]
	if !got.Settled || got.CostMicroUSD != 17 || got.Recipe.Hash() != o.Recipe.Hash() || got.Results[s.Items[0].Key].Outcome != "no_family" || len(got.attempts) != 0 {
		t.Fatalf("historical settlement changed: %+v", got)
	}
}

func TestResultStore_ReplaysKnownItemWithoutReleasingIncompleteBatch(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	docs := append(documents(), Document{Key: "vn:vbpl:2", Title: "Other synthetic document"})
	o.Documents = len(docs)
	f.beforeCreate = func(requests []batchapi.Request) {
		for _, req := range requests {
			b, err := req.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), docs[0].Key) {
				f.results = []batchapi.Result{storeResult(t, req.CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[\"1.1\"]}"}],"usage":{"input_tokens":100,"output_tokens":10}}`)}
			}
		}
	}
	if _, err := r.Screen(ctx, o, docs, families()); err == nil || ledger.rows[0].Settled {
		t.Fatalf("incomplete submission settled: %v", err)
	}
	reserve := ledger.rows[0].ReserveMicroUSD
	o.RunID++
	o.Documents = 1
	o.Out = filepath.Join(t.TempDir(), "replay")
	res, err := r.Screen(ctx, o, docs[:1], families())
	if err != nil || res.Requests != 0 || f.creates != 1 || res.LedgerMicroUSD != reserve || ledger.rows[0].Settled {
		t.Fatalf("replay %+v, creates %d, error %v", res, f.creates, err)
	}
}

func TestResultStore_MixedReplayRetainsBothOutcomesAfterRebuild(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	if _, err := r.Screen(ctx, o, documents(), families()); err != nil {
		t.Fatal(err)
	}
	docs := append(documents(), Document{Key: "vn:vbpl:2", Title: "Other synthetic document"})
	o.RunID++
	o.Documents = 2
	o.Out = filepath.Join(t.TempDir(), "mixed")
	f.beforeCreate = func(requests []batchapi.Request) {
		if len(requests) != 1 {
			t.Fatalf("submitted %d requests, want one", len(requests))
		}
		last := ledger.rows[1]
		for i, item := range last.Items {
			if !item.Local && requests[0].CustomID != customID(last.ID, i) {
				t.Fatalf("custom ID %s does not match the transmitted item index %d", requests[0].CustomID, i)
			}
		}
		f.results = []batchapi.Result{{CustomID: requests[0].CustomID, Type: "expired"}}
	}
	if _, err := r.Screen(ctx, o, docs, families()); err != nil {
		t.Fatal(err)
	}
	before := ledger.rows[1].Results
	ledger.rows = nil
	if err := r.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	for _, got := range ledger.rows {
		if got.RunID != o.RunID {
			continue
		}
		if !got.Settled || got.CostMicroUSD != 0 || len(got.attempts) != 1 || len(got.Results) != 2 || got.Results[docs[0].Key].Outcome != "ok" || got.Results[docs[1].Key].Outcome != "invalid_response" || len(before) != len(got.Results) {
			t.Fatalf("rebuilt mixed outcomes: %+v", got)
		}
		return
	}
	t.Fatal("missing mixed submission after rebuild")
}

func TestResultStore_RebuildKeepsAbandonedKeyLockedWhenReceiptAppears(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.beforeCreate = nil
	f.createErr = context.DeadlineExceeded
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected uncertain create")
	}
	s := ledger.rows[0]
	r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
	if err := r.Reconcile(ctx, s.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := r.recordBatch(&s, "batch_late"); err != nil {
		t.Fatal(err)
	}
	ledger.rows = nil
	if err := r.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	got := ledger.rows[0]
	if got.Settled || got.BatchID != "batch_late" || nonterminal(got.State) {
		t.Fatalf("rebuilt late receipt cannot coexist with a replacement screen: %+v", got)
	}
	o.RunID++
	o.Out = filepath.Join(t.TempDir(), "locked")
	if res, err := r.Screen(ctx, o, documents(), families()); err != nil || res.UnknownKeys != 1 || f.creates != 1 {
		t.Fatalf("late receipt did not lock key: summary=%+v, creates=%d, error=%v", res, f.creates, err)
	}
}

func TestResultStore_DoesNotPriceMalformedCacheBreakdown(t *testing.T) {
	r, ledger, f, o := screenFixture(t)
	f.beforeCreate = func(req []batchapi.Request) {
		f.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}"}],"usage":{"input_tokens":10,"cache_creation_input_tokens":60,"cache_creation":"invalid","output_tokens":10}}`)}
	}
	if _, err := r.Screen(context.Background(), o, documents(), families()); err == nil || ledger.rows[0].Settled {
		t.Fatalf("malformed cache breakdown settled the reserve: %v", err)
	}
}
