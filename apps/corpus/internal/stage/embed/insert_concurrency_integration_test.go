//go:build integration

package embed

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/store"
	"danny.vn/trachuan/apps/corpus/internal/testdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type firstInsertGate struct {
	mu    sync.Mutex
	count int
	both  chan struct{}
}
type gatedTx struct {
	pgx.Tx
	gate  *firstInsertGate
	first bool
}

func (tx *gatedTx) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, sql, args...)
	if err != nil || tx.first {
		return tag, err
	}
	tx.first = true
	tx.gate.mu.Lock()
	tx.gate.count++
	if tx.gate.count == 2 {
		close(tx.gate.both)
	}
	tx.gate.mu.Unlock()
	select {
	case <-tx.gate.both:
	case <-time.After(250 * time.Millisecond):
	case <-ctx.Done():
		return tag, ctx.Err()
	}
	return tag, nil
}

func TestWriteRows_ConcurrentOppositeOrdersCommit(t *testing.T) {
	url := testdb.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ids := make([]int64, 3)
	for i := range ids {
		if err = db.QueryRow(ctx, "INSERT INTO artifact(stage,item_key,input_hash,recipe_hash,path,sha256) VALUES('embed',$1,$1,'recipe',$1,$1) RETURNING id", fmt.Sprint(i)).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(ctx, "INSERT INTO embed_row(recipe_hash,input_sha256,artifact_id,row,token_count) VALUES('recipe','a',$1,0,0)", ids[2]); err != nil {
		t.Fatal(err)
	}
	incumbents := map[string]incumbentRow{"a": {row: store.LookupEmbedRowsRow{ArtifactID: ids[2]}, stale: true}}
	gate := &firstInsertGate{both: make(chan struct{})}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i, rows := range [][]Row{{{InputSHA256: "a", TokenCount: 1}, {InputSHA256: "b", TokenCount: 1}}, {{InputSHA256: "b", TokenCount: 1}, {InputSHA256: "a", TokenCount: 1}}} {
		tx, beginErr := db.Begin(ctx)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		go func(id int64, rows []Row, tx pgx.Tx) {
			defer func() { _ = tx.Rollback(context.Background()) }()
			<-start
			_, e := writeRows(ctx, store.New(&gatedTx{Tx: tx, gate: gate}), "recipe", id, rows, incumbents)
			if e == nil {
				e = tx.Commit(ctx)
			}
			results <- e
		}(ids[i], rows, tx)
	}
	close(start)
	for range 2 {
		if err = <-results; err != nil {
			t.Errorf("transaction failed: %v", err)
		}
	}
	var count int
	var artifactCount int
	if err = db.QueryRow(ctx, "SELECT count(*), count(DISTINCT artifact_id) FROM embed_row WHERE recipe_hash='recipe' AND artifact_id<>$1", ids[2]).Scan(&count, &artifactCount); err != nil || count != 2 || artifactCount != 1 {
		t.Fatalf("rows=%d artifact IDs=%d err=%v", count, artifactCount, err)
	}
}
