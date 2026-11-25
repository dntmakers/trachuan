package retrieval

import (
	"testing"

	"danny.vn/trachuan/internal/retrieval/lexical"
)

func TestZeroEngineSparseLifecycle(t *testing.T) {
	e := &Engine{}
	if _, err := e.Search(t.Context(), Request{}); err == nil {
		t.Fatal("zero engine search should reject an uncalibrated recipe")
	}
	e.StartSparsePostings(nil)
	if e.SparsePostingsReady() || e.postingsFor(lexical.Vector{}) != nil || e.publishPostings(&sparsePostings{}) {
		t.Fatal("zero engine has postings")
	}
	if got := e.LexicalSources(); got != (LexicalSources{}) {
		t.Fatalf("unexpected sources: %+v", got)
	}
	e.Retire()
	e.Close()
}
