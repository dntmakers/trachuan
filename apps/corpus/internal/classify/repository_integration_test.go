//go:build integration

package classify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"danny.vn/trachuan/apps/corpus/internal/testdb"
	"danny.vn/trachuan/internal/testpg"
	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestScreen_DatabaseSettlementAndReplay(t *testing.T) {
	ctx := context.Background()
	db, err := pgxpool.New(ctx, testdb.New(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	r, _, f, o := screenFixture(t)
	o.Release = testpg.Release(t)
	if err = db.QueryRow(ctx, `INSERT INTO run(country,rel,state,snapshot) VALUES('vn',$1,'paused','{}') RETURNING id`, o.Release).Scan(&o.RunID); err != nil {
		t.Fatal(err)
	}
	r.Repository = &PGRepository{Pool: db}
	f.beforeCreate = func(requests []batchapi.Request) {
		var id, state string
		var reserve int64
		if queryErr := db.QueryRow(ctx, `SELECT id,state,reserved_micro_usd FROM submission WHERE run_id=$1`, o.RunID).Scan(&id, &state, &reserve); queryErr != nil {
			t.Fatal(queryErr)
		}
		if state != "preparing" || reserve != 1031 || requests[0].CustomID != id+"-000000" {
			t.Fatal("submission was not reserved before create")
		}
		if _, statErr := os.Stat(filepath.Join(r.IntentDir, "submissions", id+".json")); statErr != nil {
			t.Fatal(statErr)
		}
		var message anthropic.Message
		if decodeErr := json.Unmarshal([]byte(`{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[\"1.1\"]}"}],"usage":{"input_tokens":100,"output_tokens":10}}`), &message); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		f.results = []batchapi.Result{{CustomID: requests[0].CustomID, Type: "succeeded", Message: message}}
	}
	if _, err = r.Screen(ctx, o, documents(), families()); err != nil {
		t.Fatal(err)
	}
	var state string
	var cost int64
	var settled bool
	if err = db.QueryRow(ctx, `SELECT state,cost_micro_usd,cost_settled FROM submission WHERE run_id=$1`, o.RunID).Scan(&state, &cost, &settled); err != nil {
		t.Fatal(err)
	}
	if state != "completed" || cost != 8 || !settled {
		t.Fatalf("state %s cost %d settled %t", state, cost, settled)
	}
	assertAttempt := func() {
		t.Helper()
		var outcome, version string
		var attemptCost int64
		if queryErr := db.QueryRow(ctx, `SELECT a.outcome,a.cost_micro_usd,a.price_version FROM classification_attempt a JOIN submission s ON s.id=a.submission_id WHERE s.run_id=$1 AND a.item_index=0 AND s.price_version=a.price_version`, o.RunID).Scan(&outcome, &attemptCost, &version); queryErr != nil {
			t.Fatal(queryErr)
		}
		if outcome != "final" || attemptCost != 8 || version != batchPriceVersion {
			t.Fatalf("attempt %s cost %d version %s", outcome, attemptCost, version)
		}
	}
	assertAttempt()
	if _, err = db.Exec(ctx, `DELETE FROM classification_attempt`); err != nil {
		t.Fatal(err)
	}
	if err = r.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	assertAttempt()

	if _, err = r.Screen(ctx, o, documents(), families()); err != nil {
		t.Fatal(err)
	}
	if f.creates != 1 || f.downloads != 1 {
		t.Fatal("replayed provider operations after settlement")
	}
}

func TestRepository_RestoreRequiresMatchingBudgetPair(t *testing.T) {
	ctx := context.Background()
	db, err := pgxpool.New(ctx, testdb.New(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	var runID int64
	if err = db.QueryRow(ctx, `INSERT INTO run(country,rel,state,snapshot) VALUES('vn',$1,'paused','{}') RETURNING id`, testpg.Release(t)).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	repo := &PGRepository{Pool: db}
	unlock, err := repo.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unlock)
	recipe, err := NewRecipe("claude-fable-5-2", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	items := []Item{{Key: "vn:vbpl:1", RequestHash: strings.Repeat("a", 64), InputTokens: 100}}
	for _, tc := range []struct {
		name, digit, budget string
		amount              int64
		wantViolation       bool
	}{
		{"valid trial", "a", "trial", 200000000, false},
		{"pilot with trial amount", "b", "pilot", 200000000, true},
		{"trial with pilot amount", "c", "trial", 15000000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Submission{ID: strings.Repeat(tc.digit, 32), RunID: runID, ScreenHash: strings.Repeat(tc.digit, 64), Recipe: recipe, Items: items, ItemsHash: itemsHash(items), State: "uncertain", CreatedAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), Budget: tc.budget, BudgetMicroUSD: tc.amount, ReserveMicroUSD: 82320}
			err := repo.Restore(ctx, []Submission{s})
			if !tc.wantViolation {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "submission_budget_pair_check" {
				t.Fatalf("restore error = %v; want submission_budget_pair_check violation", err)
			}
		})
	}
}

func TestRepository_SerializesBudgetAdmission(t *testing.T) {
	ctx := context.Background()
	db, err := pgxpool.New(ctx, testdb.New(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	first := &PGRepository{Pool: db}
	second := &PGRepository{Pool: db}
	unlock, err := first.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if release, lockErr := second.Lock(waitCtx); lockErr == nil {
		release()
		unlock()
		t.Fatal("two commands acquired budget admission lock")
	}
	unlock()
	release, err := second.Lock(ctx)
	if err != nil {
		t.Fatal("budget lock was not released", err)
	}
	release()
}

func TestRepository_WorkshopMoneyDriftBlocksCreate(t *testing.T) {
	for _, field := range []string{"reserve", "cost"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			db, runID, rel := submissionDatabase(t)
			r, _, f, o := screenFixture(t)
			r.Repository = &PGRepository{Pool: db}
			o.RunID, o.Release = runID, rel
			f.ended = field == "cost"
			var id string
			f.beforeCreate = func(req []batchapi.Request) {
				id = strings.SplitN(req[0].CustomID, "-", 2)[0]
				f.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}"}],"usage":{"input_tokens":100,"output_tokens":10}}`)}
			}
			if _, err := r.Screen(ctx, o, documents(), families()); (err == nil) != f.ended || id == "" {
				t.Fatalf("unexpected first submission: %v", err)
			}
			query := `UPDATE submission SET reserved_micro_usd=0 WHERE id=$1`
			if field == "cost" {
				query = `UPDATE submission SET cost_micro_usd=0 WHERE id=$1`
			}
			if _, err := db.Exec(ctx, query, id); err != nil {
				t.Fatal(err)
			}
			t.Run("next release", func(t *testing.T) {
				o.RunID, o.Release = submissionRun(t, db)
				o.Out = t.TempDir()
				if _, err := r.Screen(ctx, o, []Document{{Key: "vn:vbpl:2", Title: "Other synthetic document"}}, families()); err == nil || !strings.Contains(err.Error(), field) || f.creates != 1 {
					t.Fatalf("workshop %s drift did not stop Create: creates=%d, error=%v", field, f.creates, err)
				}
			})
		})
	}
}

func TestRepository_ReusesUnlockedLedgerConnectionWithoutRedial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg, err := pgxpool.ParseConfig(testdb.New(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns, cfg.MinConns, cfg.MinIdleConns = 1, 0, 0
	var connects atomic.Int32
	cfg.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
		if connects.Add(1) != 1 {
			return errors.New("test prohibits opening a second PostgreSQL connection")
		}
		return nil
	}
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	first := &PGRepository{Pool: db}
	release, err := first.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	release()
	var held bool
	if err = db.QueryRow(context.Background(), `SELECT EXISTS (
		SELECT 1 FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid() AND objsubid=1 AND granted
		AND classid::bigint=(($1::bigint >> 32) & 4294967295) AND objid::bigint=($1::bigint & 4294967295)
	)`, submissionLedgerLockKey).Scan(&held); err != nil || held {
		t.Fatalf("observer could not reuse an unlocked session: opens=%d, held=%t, error=%v", connects.Load(), held, err)
	}
	second := &PGRepository{Pool: db}
	release, err = second.Lock(context.Background())
	if err != nil {
		t.Fatalf("observer could not reuse the unlocked session: opens=%d, %v", connects.Load(), err)
	}
	defer release()
	if _, err = second.List(context.Background()); err != nil || connects.Load() != 1 {
		t.Fatalf("reused session could not list: opens=%d, error=%v", connects.Load(), err)
	}
}
