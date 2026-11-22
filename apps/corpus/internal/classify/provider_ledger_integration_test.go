//go:build integration

package classify

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/testdb"
	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProviderLedger_RollbackProvider(t *testing.T) {
	for _, openai := range []bool{false, true} {
		t.Run(fmt.Sprint(openai), func(t *testing.T) {
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
			insert := `INSERT INTO submission (id,run_id,stage,provider,project_id,screen_hash,recipe,families,item_list,item_list_hash,state,created_at,budget,budget_micro_usd,reserved_micro_usd) VALUES ($1,$2,'classify',$3,$4,$5,'{}','[]','[]',$5,'abandoned',now(),$6,$7,0)`
			if _, err = db.Exec(ctx, insert, strings.Repeat("a", 32), runID, nil, nil, strings.Repeat("a", 64), "pilot", int64(15000000)); err != nil {
				t.Fatal(err)
			}
			if openai {
				if _, err = db.Exec(ctx, insert, strings.Repeat("b", 32), runID, "openai", "proj-test", strings.Repeat("b", 64), "openai-trial", int64(40000000)); err != nil {
					t.Fatal(err)
				}
			}
			migration, err := os.ReadFile("../../migrations/00017_submission_provider.sql")
			if err != nil {
				t.Fatal(err)
			}
			_, down, ok := strings.Cut(string(migration), "-- +goose Down")
			if !ok {
				t.Fatal("missing down migration")
			}
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, down); err != nil {
				if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
					t.Fatal(rollbackErr)
				}
				if !openai {
					t.Fatal(err)
				}
			} else {
				if openai {
					_ = tx.Rollback(ctx)
					t.Fatal("accepted OpenAI rollback")
				}
				if err = tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var provider *string
			if err = db.QueryRow(ctx, `SELECT provider FROM submission WHERE id=$1`, strings.Repeat("a", 32)).Scan(&provider); err != nil {
				t.Fatal(err)
			}
			if openai && provider != nil {
				t.Fatal("failed rollback changed legacy provider")
			}
			if !openai && (provider == nil || *provider != "anthropic") {
				t.Fatal("legacy provider not restored")
			}
		})
	}
}

func TestProviderLedger_DatabaseMigrationAndTuples(t *testing.T) {
	ctx := context.Background()
	db, err := pgxpool.New(ctx, testdb.New(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	migration, err := os.ReadFile("../../migrations/00017_submission_provider.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(migration), "-- +goose Down")
	if !ok {
		t.Fatal("missing migration down section")
	}
	if _, err = db.Exec(ctx, down); err != nil {
		t.Fatal(err)
	}
	var runID int64
	if err = db.QueryRow(ctx, `INSERT INTO run(country,rel,state,snapshot) VALUES('vn',$1,'paused','{}') RETURNING id`, testpg.Release(t)).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO submission (id,run_id,stage,provider,project_id,screen_hash,recipe,families,item_list,item_list_hash,state,created_at,budget,budget_micro_usd,reserved_micro_usd) VALUES ($1,$2,'classify',$3,$4,$5,'{}','[]','[]',$5,'abandoned',now(),$6,$7,0)`
	legacyInsert := `INSERT INTO submission (id,run_id,stage,provider,screen_hash,recipe,families,item_list,item_list_hash,state,created_at,budget,budget_micro_usd,reserved_micro_usd) VALUES ($1,$2,'classify','anthropic',$3,'{}','[]','[]',$3,'abandoned',now(),'pilot',15000000,0)`
	if _, err = db.Exec(ctx, legacyInsert, strings.Repeat("a", 32), runID, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, up); err != nil {
		t.Fatal(err)
	}
	var legacy string
	if err = db.QueryRow(ctx, `SELECT provider FROM submission WHERE id=$1`, strings.Repeat("a", 32)).Scan(&legacy); err != nil || legacy != "anthropic" {
		t.Fatalf("legacy provider %q: %v", legacy, err)
	}
	for i, tc := range []struct {
		provider, project any
		budget            string
		amount            int64
		valid             bool
	}{
		{nil, nil, "pilot", 15000000, true},
		{"anthropic", nil, "trial", 200000000, true},
		{"openai", "proj-test", "openai-trial", 40000000, true},
		{"openai", "proj-test", "openai-full", 175000000, true},
		{nil, nil, "openai-trial", 40000000, false},
		{"openai", nil, "openai-trial", 40000000, false},
		{"openai", "bad project", "openai-trial", 40000000, false},
		{"anthropic", "proj-test", "trial", 200000000, false},
		{"openai", "proj-test", "trial", 200000000, false},
	} {
		id := fmt.Sprintf("%032x", i+1)
		hash := fmt.Sprintf("%064x", i+1)
		_, err = db.Exec(ctx, insert, id, runID, tc.provider, tc.project, hash, tc.budget, tc.amount)
		if (err == nil) != tc.valid {
			t.Errorf("tuple %+v: %v", tc, err)
		}
	}
}
