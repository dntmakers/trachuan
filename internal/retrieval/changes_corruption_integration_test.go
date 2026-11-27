//go:build integration

package retrieval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
)

func TestChanges_InvalidLookaheadFailsClosed(t *testing.T) {
	db, e := readFixture(t)
	e.release.Country = "vn"
	e.release.SchemaVersion = 9
	key, previous, hash := "vn:1", "20991109", strings.Repeat("a", 64)
	rows := []releasechanges.Row{{Ordinal: 1, Kind: "document_added", Key: &key, Payload: `{"cause":"membership","key":"vn:1"}`}, {Ordinal: 2, Kind: "document_added", Key: &key, Payload: `{"cause":"source","key":"vn:1"}`}}
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = corpusdb.WriteReleaseChanges(ctx, tx, releasechanges.Header{Version: releasechanges.Version, Country: "vn", Release: e.release.Rel, Predecessor: &previous, PredecessorIdentityHash: &hash}, rows); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	readExec(t, db, `UPDATE release_change SET key2='vn:wrong' WHERE ordinal=2`)
	limit := 1
	_, err = e.Changes(ctx, ChangesRequest{Country: "vn", Limit: &limit})
	var invalid *corpusdb.InvalidProjectionError
	if !errors.Is(err, ErrUnavailable) || !errors.As(err, &invalid) {
		t.Fatalf("lookahead error = %v", err)
	}
	readExec(t, db, `UPDATE release_change SET key2=NULL WHERE ordinal=2`)
	readExec(t, db, `UPDATE release_change_set SET predecessor_identity_hash=NULL`)
	if _, err = e.Changes(ctx, ChangesRequest{Country: "vn"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing identity hash: %v", err)
	}
	readExec(t, db, `UPDATE release_change_set SET predecessor_identity_hash=$1`, hash)
	readExec(t, db, `UPDATE release_change SET ordinal=3 WHERE ordinal=2`)
	if _, err = e.Changes(ctx, ChangesRequest{Country: "vn"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("ordinal beyond counts: %v", err)
	}
}
