//go:build integration

package cli

import (
	"context"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5"
)

func TestHeadingRelease_AcceptsSchema8And9(t *testing.T) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testpg.NewDatabase(t, "vector", "ltree"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	if err = corpusdb.ApplyTables(ctx, conn, 1024); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	if _, err = conn.Exec(ctx, `INSERT INTO release VALUES(true,'vn','20990101',9,'','{}','{}','{}','{}','{}',$1,$1,$1,'{}',now())`, hash); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{9, 8, 7} {
		if version == 8 {
			if _, err = conn.Exec(ctx, `ALTER TABLE release DROP CONSTRAINT release_schema_version_check; UPDATE release SET schema_version=8`); err != nil {
				t.Fatal(err)
			}
		}
		if version == 7 {
			if _, err = conn.Exec(ctx, `UPDATE release SET schema_version=7`); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err = headingRelease(ctx, conn, "vn", "20990101")
		if (err == nil) != (version >= 8) {
			t.Fatalf("version %d: %v", version, err)
		}
	}
}
