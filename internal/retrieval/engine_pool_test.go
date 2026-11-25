package retrieval

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEngine_WithPoolSharesPostings(t *testing.T) {
	original := &Engine{db: &pgxpool.Pool{}, sparse: &sparseLifecycle{}}
	original.sparse.init()
	defer original.Close()
	postings := &sparsePostings{}
	if !original.publishPostings(postings) {
		t.Fatal("publish postings")
	}
	pool := &pgxpool.Pool{}
	copy := original.WithPool(pool)
	if copy.db != pool || original.db == pool {
		t.Fatal("pool was not replaced on the copy only")
	}
	if copy.sparse.index.Load() != postings {
		t.Fatal("copy does not share postings")
	}
	original.sparse.index.Store(nil)
	if copy.sparse.index.Load() != nil {
		t.Fatal("copy does not share postings lifecycle")
	}
}
