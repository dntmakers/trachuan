package classify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

func TestResultStore_OrphanedEvidenceBlocksRebuildAndAdmission(t *testing.T) {
	for _, suffix := range []string{"batch", "settled", "attempt"} {
		t.Run(suffix, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, f, o := screenFixture(t)
			docs := append(documents(), Document{Key: "vn:vbpl:2", Title: "Other synthetic document"})
			f.beforeCreate = func(req []batchapi.Request) {
				f.results = []batchapi.Result{{CustomID: req[0].CustomID, Type: "expired"}}
			}
			for i, d := range docs {
				o.RunID = int64(i + 1)
				o.Out = filepath.Join(t.TempDir(), "result")
				if _, err := r.Screen(ctx, o, []Document{d}, families()); err != nil {
					t.Fatal(err)
				}
			}
			lost := ledger.rows[0]
			base := filepath.Join(r.IntentDir, "submissions", lost.ID)
			for kind, path := range map[string]string{"intent": base + ".json", "batch": base + ".batch", "settled": base + ".settled", "attempt": r.attemptPath(lost, 0), "creator_lock": base + ".creator.lock", "creator_finished": base + ".creator.finished", "creator_dispatch": base + ".creator.dispatch"} {
				if kind != suffix {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
			}
			ledger.rows = ledger.rows[1:]
			if err := r.Rebuild(ctx); err == nil {
				t.Error("rebuild ignored one missing intent")
			}
			o.RunID++
			o.Out = filepath.Join(t.TempDir(), "another")
			if _, err := r.Screen(ctx, o, docs[:1], families()); err == nil || f.creates != 2 {
				t.Errorf("admission ignored one missing intent: creates %d, error %v", f.creates, err)
			}
		})
	}
}

func TestResultStore_RejectsCoercedUsage(t *testing.T) {
	for _, usage := range []string{
		`{"input_tokens":true,"output_tokens":10}`,
		`{"input_tokens":100,"output_tokens":1.5}`,
		`{"input_tokens":100,"cache_read_input_tokens":false,"output_tokens":10}`,
		`{"input_tokens":100,"cache_creation_input_tokens":3,"cache_creation":{"ephemeral_1h_input_tokens":false,"ephemeral_5m_input_tokens":3.0},"output_tokens":10}`,
		`{"input_tokens":100,"cache_creation_input_tokens":3,"cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":3,"ephemeral_6h_input_tokens":1},"output_tokens":10}`,
	} {
		t.Run(usage, func(t *testing.T) {
			r, ledger, f, o := screenFixture(t)
			f.beforeCreate = func(req []batchapi.Request) {
				f.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}"}],"usage":`+usage+`}`)}
			}
			if _, err := r.Screen(context.Background(), o, documents(), families()); err == nil || ledger.rows[0].Settled {
				t.Fatalf("coerced usage settled reserve: inserted cost %d, error %v", ledger.rows[0].CostMicroUSD, err)
			}
		})
	}
}

func TestResultStore_ReconcilePersistsEndedBatchResults(t *testing.T) {
	for _, n := range []int{1, 2} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			ctx := context.Background()
			r, ledger, f, o := screenFixture(t)
			o.Documents = 2
			docs := append(documents(), Document{Key: "vn:vbpl:2", Title: "Other synthetic document"})
			f.createErr = context.DeadlineExceeded
			f.beforeCreate = nil
			if _, err := r.Screen(ctx, o, docs, families()); err == nil {
				t.Fatal("expected uncertain batch")
			}
			s := ledger.rows[0]
			for i := 0; i < n; i++ {
				f.results = append(f.results, batchapi.Result{CustomID: customID(s.ID, i), Type: "expired"})
			}
			_ = r.Reconcile(ctx, s.ID, false)
			s = ledger.rows[0]
			if _, err := os.Stat(r.attemptPath(s, 0)); err != nil || s.BatchID == "" || s.Settled != (n == 2) {
				t.Fatalf("reconcile did not persist available evidence: receipt %q settled %t, file error %v", s.BatchID, s.Settled, err)
			}
		})
	}
}

func TestResultStore_OnlyOneCompleteCaptureSettles(t *testing.T) {
	for _, route := range []string{"split_downloads", "resume", "rebuild"} {
		t.Run(route, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, f, o := screenFixture(t)
			o.Documents = 2
			docs := append(documents(), Document{Key: "vn:vbpl:2", Title: "Other synthetic document"})
			f.beforeCreate = func(req []batchapi.Request) {
				f.results = []batchapi.Result{{CustomID: req[0].CustomID, Type: "expired"}}
			}
			if _, err := r.Screen(ctx, o, docs, families()); err == nil {
				t.Fatal("expected incomplete results")
			}
			s := ledger.rows[0]
			if route == "split_downloads" {
				f.results = []batchapi.Result{{CustomID: customID(s.ID, 1), Type: "expired"}}
			} else {
				f.results = []batchapi.Result{{CustomID: customID(s.ID, 0), Type: "expired"}, {CustomID: customID(s.ID, 1), Type: "expired"}}
				if err := r.captureResults(ctx, s.ID, s.BatchID, time.Time{}); err != nil {
					t.Fatal(err)
				}
				r.Client = nil
			}
			before, downloads := f.gets, f.downloads
			var err error
			if route == "rebuild" {
				ledger.rows = nil
				err = r.Rebuild(ctx)
			} else {
				_, err = r.Screen(ctx, o, docs, families())
			}
			if route == "split_downloads" {
				if err == nil || ledger.rows[0].Settled || len(ledger.rows[0].attempts) != 2 {
					t.Fatalf("split captures released reserve or did not record all attempts: %v", err)
				}
				return
			}
			if err != nil || len(ledger.rows) != 1 || !ledger.rows[0].Settled || ledger.rows[0].CostMicroUSD != 0 || f.gets != before || f.downloads != downloads {
				t.Fatalf("retained settlement failed: rows %+v, get %d, downloads %d, error %v", ledger.rows, f.gets, f.downloads, err)
			}
		})
	}
}

func TestResultStore_HeldKeysLeaveGapsWithoutBlockingIndependentWork(t *testing.T) {
	for _, state := range []string{"submitted", "failed", "rebuilt_deadline"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, f, o := screenFixture(t)
			f.ended = false
			f.beforeCreate = nil
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
				t.Fatal("expected interrupted polling")
			}
			if state != "submitted" {
				deadline := ledger.rows[0].DeadlineAt.Add(time.Second)
				r.Now = func() time.Time { return deadline }
				if _, err := r.Screen(ctx, o, documents(), families()); err == nil || ledger.rows[0].State != "failed" {
					t.Fatal("expected deadline to hold reserve")
				}
				if state == "rebuilt_deadline" {
					ledger.rows = nil
					if err := r.Rebuild(ctx); err != nil {
						t.Fatal(err)
					}
				}
			}
			reserve := ledger.rows[0].ReserveMicroUSD
			f.ended = true
			f.beforeCreate = func(req []batchapi.Request) {
				if len(req) != 1 || !strings.Contains(req[0].CustomID, ledger.rows[1].ID) {
					t.Fatalf("sent held request with independent work: %+v", req)
				}
				f.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[\"1.1\"]}"}],"usage":{"input_tokens":100,"output_tokens":10}}`)}
			}
			o.RunID++
			o.Release = "20990102"
			o.Out = filepath.Join(t.TempDir(), "independent")
			o.Documents = 2
			other := Document{Key: "vn:vbpl:2", Title: "Other synthetic document"}
			res, err := r.Screen(ctx, o, append(documents(), other), families())
			if err != nil || res.Requests != 1 || res.LedgerMicroUSD != reserve || (state != "submitted" && res.UnknownKeys != 1) || (state == "submitted" && res.HeldKeys != 1) || len(ledger.rows) != 2 || ledger.rows[0].Settled || ledger.rows[1].Results[documents()[0].Key].Outcome != "invalid_response" || ledger.rows[1].Results[other.Key].Outcome != "ok" {
				t.Fatalf("independent work blocked or charged held key: %+v, error %v", res, err)
			}
		})
	}
}

const articleDatabaseReserve = 2055
const replacementFixtureInput = 32000

func TestResultStore_IntegrationReservesExerciseTheirScenarios(t *testing.T) {
	article := articleRecipe(t)
	if reserve, err := article.Reserve(100); err != nil || reserve != articleDatabaseReserve {
		t.Errorf("article database reserve assertion = %d, actual %d, error %v", articleDatabaseReserve, reserve, err)
	}
	sonnet, _ := NewRecipe("claude-sonnet-5-5", article.TaxonomyHash)
	item, err := sonnet.Reserve(replacementFixtureInput)
	if err != nil || 2*124*item > BudgetMicroUSD("pilot") || 2*124*item+1031 <= BudgetMicroUSD("pilot") {
		t.Errorf("replacement fixture cannot distinguish two reserves from a third: item reserve %d, error %v", item, err)
	}
}

func TestResultStore_ArticleGroupReplaysWithoutProviderAfterRebuild(t *testing.T) {
	ctx := context.Background()
	r, ledger, _, o, artifact := runArticlesFixture(t)
	articles := articleInputs(2, "one-chapter")
	if _, err := r.Articles(ctx, o, "labels", articles, artifact, articleNodes()); err != nil {
		t.Fatal(err)
	}
	ledger.rows = nil
	if err := r.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	r.Client = nil
	o.RunID++
	o.Out = filepath.Join(t.TempDir(), "replay")
	if res, err := r.Articles(ctx, o, "labels", articles, artifact, articleNodes()); err != nil || res.Requests != 0 || len(ledger.rows) != 1 {
		t.Fatalf("article replay needed token count provider: summary %+v, error %v", res, err)
	}
}
