//go:build integration

package build

import (
	"context"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/corpusdb"
)

func TestDatabaseMetrics_SparseOutOfLine(t *testing.T) {
	f := fixture(t)
	ctx := context.Background()
	hash := strings.Repeat("a", 64)
	if _, err := f.corpus.Exec(ctx, `INSERT INTO release VALUES (true,'vn','20990101_1',$2,'','{}','{}','{}','{}','{}',$1,$1,$1,'{}',now())`, hash, corpusdb.SchemaVersion); err != nil {
		t.Fatal(err)
	}
	counts, _, _, err := databaseMetrics(ctx, f.corpus, Counts{})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := counts["sparse_out_of_line"]; !ok || got != 0 {
		t.Fatalf("sparse_out_of_line = %d, present = %t, want 0", got, ok)
	}
}
