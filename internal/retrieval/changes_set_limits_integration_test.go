//go:build integration

package retrieval

import (
	"context"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/corpusdb"
)

func TestChangesSet_OversizedFieldsNotTransferred(t *testing.T) {
	for _, column := range []string{"country", "release", "predecessor", "predecessor_identity_hash", "digest"} {
		t.Run(column, func(t *testing.T) {
			db, _ := readFixture(t)
			ctx := context.Background()
			readExec(t, db, `INSERT INTO release_change_set (version,country,release,predecessor,predecessor_identity_hash,baseline,document_added,document_removed,document_validity,provision_validity,provision_text,document_key,digest) VALUES(1,'vn','20991110','20991109',repeat('a',64),false,0,0,0,0,0,0,repeat('a',64))`)
			for _, constraint := range map[string][]string{"country": {"release_change_set_country_length"}, "release": {"release_change_set_release_length"}, "predecessor": {"release_change_set_predecessor_length"}, "predecessor_identity_hash": {"release_change_set_predecessor_identity_hash_check"}, "digest": {"release_change_set_digest_check"}}[column] {
				readExec(t, db, `ALTER TABLE release_change_set DROP CONSTRAINT `+constraint)
			}
			readExec(t, db, `UPDATE release_change_set SET `+column+`=$1`, strings.Repeat("a", 10000))
			var version, n int
			var country, rel, previous, identity, digest *string
			var baseline, ok bool
			var counts [6]int64
			err := db.QueryRow(ctx, changesSetSQL, corpusdb.ReleaseChangeLimits().TotalBytes).Scan(&version, &country, &rel, &previous, &identity, &baseline, &counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &digest, &n, &ok)
			if err != nil || ok || country != nil || rel != nil || previous != nil || identity != nil || digest != nil {
				t.Fatalf("oversized set transferred: ok=%v err=%v", ok, err)
			}
		})
	}
}
