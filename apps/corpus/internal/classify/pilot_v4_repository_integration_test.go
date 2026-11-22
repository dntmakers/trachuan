//go:build integration

package classify

import (
	"context"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/testdb"
	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPilotV4_WorkshopListRejectsLegacyPresence(t *testing.T) {
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
	recipe, err = NewArticleRecipe(recipe, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	items := []Item{{Key: "synthetic", RequestHash: strings.Repeat("a", 64), InputTokens: 100, Article: &ArticleRequest{DocumentKey: "synthetic", Articles: []Article{}, Nodes: []Node{{Code: "A"}}}}}
	s := Submission{ID: strings.Repeat("a", 32), RunID: runID, ScreenHash: strings.Repeat("a", 64), Recipe: recipe, Items: items, ItemsHash: itemsHash(items), State: "uncertain", CreatedAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), Budget: "trial", BudgetMicroUSD: 200000000, ReserveMicroUSD: 82320, KeyVersion: keyVersion, PriceVersion: batchPriceVersion}
	if err = repo.Restore(ctx, []Submission{s}); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.List(ctx); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	for _, tc := range []struct{ name, path, value string }{
		{"title", "{0,article_request,document_title}", `""`},
		{"codes", "{0,article_request,document_codes}", `[]`},
		{"prompt", "{0,article_request,prompt_version}", `0`},
		{"provisions", "{0,article_request,allowed_subtrees,0,has_provisions}", `false`},
		{"ordinal", "{0,article_request,allowed_subtrees,0,ordinal}", `0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.Exec(ctx, `UPDATE submission SET item_list=jsonb_set(item_list,$1::text[],$2::jsonb) WHERE id=$3`, tc.path, tc.value, s.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.List(ctx); err == nil {
				t.Fatal("accepted legacy field")
			}
			if _, err := db.Exec(ctx, `UPDATE submission SET item_list=item_list #- $1::text[] WHERE id=$2`, tc.path, s.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := db.Exec(ctx, `UPDATE submission SET recipe=jsonb_set(recipe,'{article,prompt_version}','4') WHERE id=$1`, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.List(ctx); err == nil {
		t.Fatal("accepted prompt 4 with current versions")
	}
}
