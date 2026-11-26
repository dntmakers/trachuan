//go:build integration

package releasecheck

import (
	"context"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProjection_VerifiesSnapshot(t *testing.T) {
	ctx := context.Background()
	db, err := pgxpool.New(ctx, testpg.NewDatabase(t, "vector", "ltree"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = corpusdb.Apply(ctx, db, 1024); err != nil {
		t.Fatal(err)
	}
	previous := "20990101"
	identity := strings.Repeat("b", 64)
	header := releasechanges.Header{Version: 1, Country: "vn", Release: "20990102", Predecessor: &previous, PredecessorIdentityHash: &identity}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := corpusdb.WriteReleaseChangesStream(ctx, tx, header, func(emit func(releasechanges.Row) error) error {
		return emit(releasechanges.Row{Ordinal: 1, Kind: "document_added", Payload: `{"x":1}`})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	expected := &ProjectionExpected{Version: 1, SHA256: result.Digest, Release: header.Release, Previous: previous}
	cases := []struct {
		name, sql string
		change    func(*ProjectionExpected)
		want      string
	}{
		{name: "match"},
		{name: "digest", change: func(e *ProjectionExpected) { e.SHA256 = strings.Repeat("a", 64) }, want: "projection digest mismatch"},
		{name: "version", change: func(e *ProjectionExpected) { e.Version = 2 }, want: "projection version mismatch"},
		{name: "release", change: func(e *ProjectionExpected) { e.Release = "20990103" }, want: "projection release mismatch"},
		{name: "previous", change: func(e *ProjectionExpected) { e.Previous = "20990103" }, want: "projection previous release mismatch"},
		{name: "payload", sql: `UPDATE release_change SET payload='{"x":2}'`, want: "invalid projection"},
		{name: "deleted", sql: `DELETE FROM release_change`, want: "invalid projection"},
		{name: "missing set", sql: `DELETE FROM release_change_set`, want: "invalid projection"},
		{name: "oversize", sql: `UPDATE release_change SET payload='{"x":"' || repeat('a',4096) || '"}'`, want: "invalid projection"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.sql != "" {
				if _, err := db.Exec(ctx, tc.sql); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := db.Exec(ctx, `DELETE FROM release_change; DELETE FROM release_change_set`); err != nil {
						t.Fatal(err)
					}
					tx, err := db.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if err := corpusdb.WriteReleaseChanges(ctx, tx, header, []releasechanges.Row{{Ordinal: 1, Kind: "document_added", Payload: `{"x":1}`}}); err != nil {
						t.Fatal(err)
					}
					if err := tx.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				}()
			}
			e := *expected
			if tc.change != nil {
				tc.change(&e)
			}
			result, err := checkProjection(ctx, db, 9, &e)
			if tc.want == "" {
				if err != nil || result == nil || result.Counts["document_added"] != 1 {
					t.Fatalf("projection result: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
	if _, err := checkProjection(ctx, db, 8, expected); err == nil || err.Error() != "projection arguments forbidden" {
		t.Fatalf("schema 8: %v", err)
	}
	if _, err := checkProjection(ctx, db, 9, nil); err == nil || err.Error() != "projection arguments required" {
		t.Fatalf("schema 9: %v", err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM release_change; DELETE FROM release_change_set`); err != nil {
		t.Fatal(err)
	}
	baseline := releasechanges.Header{Version: 1, Country: "vn", Release: "20990102", Baseline: true}
	tx, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	baseResult, err := corpusdb.WriteReleaseChangesStream(ctx, tx, baseline, func(func(releasechanges.Row) error) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	baseExpected := &ProjectionExpected{Version: 1, SHA256: baseResult.Digest, Release: baseline.Release}
	baseReport, err := checkProjection(ctx, db, 9, baseExpected)
	if err != nil || baseReport.Previous != nil || baseReport.Counts["document_added"] != 0 {
		t.Fatalf("baseline: %v", err)
	}
	baseExpected.Previous = previous
	if _, err = checkProjection(ctx, db, 9, baseExpected); err == nil || err.Error() != "projection previous release mismatch" {
		t.Fatalf("baseline previous: %v", err)
	}
	if _, err = db.Exec(ctx, `DELETE FROM release_change_set`); err != nil {
		t.Fatal(err)
	}
	baseline.Predecessor = &previous
	tx, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	baseResult, err = corpusdb.WriteReleaseChangesStream(ctx, tx, baseline, func(func(releasechanges.Row) error) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	baseExpected.SHA256 = baseResult.Digest
	baseReport, err = checkProjection(ctx, db, 9, baseExpected)
	if err != nil || !baseReport.Baseline || baseReport.Previous == nil || *baseReport.Previous != previous {
		t.Fatalf("baseline with predecessor: %v", err)
	}
	baseExpected.Previous = ""
	if _, err = checkProjection(ctx, db, 9, baseExpected); err == nil || err.Error() != "projection previous release mismatch" {
		t.Fatalf("baseline absent predecessor: %v", err)
	}
	for _, invalidHeader := range []releasechanges.Header{
		{Version: 1, Country: "vn", Release: "20990102", Predecessor: &previous},
		{Version: 1, Country: "vn", Release: "20990102", Predecessor: &previous, PredecessorIdentityHash: &identity, Baseline: true},
	} {
		if _, err = db.Exec(ctx, `DELETE FROM release_change_set`); err != nil {
			t.Fatal(err)
		}
		tx, err = db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		invalidResult, writeErr := corpusdb.WriteReleaseChangesStream(ctx, tx, invalidHeader, func(func(releasechanges.Row) error) error { return nil })
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		invalidExpected := &ProjectionExpected{Version: 1, SHA256: invalidResult.Digest, Release: invalidHeader.Release, Previous: previous}
		if _, err = checkProjection(ctx, db, 9, invalidExpected); err == nil || err.Error() != "invalid projection" {
			t.Fatalf("inconsistent baseline: %v", err)
		}
	}
	canceled, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	if _, err := checkProjection(canceled, db, 9, expected); err == nil {
		t.Fatal("accepted canceled context")
	}
}
