//go:build integration

package retrieval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
)

func TestChanges_ProjectionAndPaging(t *testing.T) {
	db, e := readFixture(t)
	e.release.SchemaVersion = 9
	e.release.Country = "vn"
	previous := "20991109"
	hash := strings.Repeat("a", 64)
	key, old := "vn:1", "vn:old"
	rows := []releasechanges.Row{
		{Ordinal: 1, Kind: "document_added", Key: &key, Payload: `{"cause":"membership","key":"vn:1"}`},
		{Ordinal: 2, Kind: "document_key", Key: &key, Key2: &old, Payload: `{"before_key":"vn:old","key":"vn:1"}`},
		{Ordinal: 3, Kind: "provision_text", Key: &key, Payload: `{"ambiguous":false,"anchor":"p5","change":"changed","key":"vn:1","provenance":false}`},
		{Ordinal: 4, Kind: "document_removed", Key: &old, Key2: &key, Payload: `{"cause":"source","key":"vn:old","replaced_by":"vn:1"}`},
		{Ordinal: 5, Kind: "document_validity", Key: &key, Key2: &old, Payload: `{"eff_to":{"after":"2024-01-01","before":null},"key":"vn:1","source_change":{"before_key":"vn:old"}}`},
		{Ordinal: 6, Kind: "provision_validity", Key: &key, Key2: &old, Payload: `{"anchor":"p5","change":"removed","key":"vn:1","source_change":{"before_key":"vn:old"},"validity":{"eff_from":null,"eff_to":null,"method":"source","status_class":"in_force"}}`},
	}
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = corpusdb.WriteReleaseChanges(ctx, tx, releasechanges.Header{Version: 1, Country: "vn", Release: e.release.Rel, Predecessor: &previous, PredecessorIdentityHash: &hash}, rows); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	all := ChangesRequest{Country: "vn", Kinds: []string{"document_added", "document_key", "provision_text"}}
	limit := 1
	all.Limit = &limit
	var got []map[string]any
	for i := 0; i < 4; i++ {
		result, err := e.Changes(ctx, all)
		if err != nil {
			t.Fatal(err)
		}
		if result.State != "available" || result.PreviousRelease == nil || *result.PreviousRelease != previous {
			t.Fatalf("result: %+v", result)
		}
		for _, raw := range result.Items {
			var item map[string]any
			if err := json.Unmarshal(raw, &item); err != nil {
				t.Fatal(err)
			}
			got = append(got, item)
		}
		if result.NextCursor == "" {
			break
		}
		all.Cursor = result.NextCursor
	}
	if len(got) != 3 || got[0]["handle"] != "vn:1" || got[1]["before_key"] != "vn:old" || got[2]["handle"] != "vn:1#p5" {
		t.Fatalf("items: %+v", got)
	}
	defaults, err := e.Changes(ctx, ChangesRequest{Country: "vn"})
	if err != nil || len(defaults.Items) != 4 || len(defaults.Counts) != 6 {
		t.Fatalf("defaults: %+v %v", defaults, err)
	}
	for _, kind := range []string{"document_added", "document_removed", "document_validity", "provision_validity", "provision_text", "document_key"} {
		page, err := e.Changes(ctx, ChangesRequest{Country: "vn", Kinds: []string{kind}})
		if err != nil || len(page.Items) != 1 || page.Counts[kind] != 1 {
			t.Fatalf("kind %s: %+v %v", kind, page, err)
		}
	}
	for _, tc := range []struct{ kind, document string }{{"document_added", key}, {"document_removed", key}, {"document_removed", old}, {"document_validity", old}, {"provision_validity", old}, {"document_key", old}, {"document_key", key}} {
		page, err := e.Changes(ctx, ChangesRequest{Country: "vn", Kinds: []string{tc.kind}, Document: tc.document})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("filter %s %s: %+v %v", tc.kind, tc.document, page, err)
		}
	}
	validity, err := e.Changes(ctx, ChangesRequest{Country: "vn", Kinds: []string{"document_validity"}})
	if err != nil || len(validity.Items) != 1 || !strings.Contains(string(validity.Items[0]), `"before":null`) {
		t.Fatalf("nullable date: %+v %v", validity, err)
	}
	for _, kind := range []string{"document_removed"} {
		page, err := e.Changes(ctx, ChangesRequest{Country: "vn", Kinds: []string{kind}})
		if err != nil || strings.Contains(string(page.Items[0]), `"handle"`) {
			t.Fatalf("removed handle %s: %+v %v", kind, page, err)
		}
	}
	removedValidity, err := e.Changes(ctx, ChangesRequest{Country: "vn", Kinds: []string{"provision_validity"}})
	if err != nil || len(removedValidity.Items) != 1 || !strings.Contains(string(removedValidity.Items[0]), `"handle":"vn:1#p5"`) {
		t.Fatalf("removed validity handle: %+v %v", removedValidity, err)
	}
	filtered, err := e.Changes(ctx, ChangesRequest{Country: "vn", Kinds: []string{"document_key"}, Document: old})
	if err != nil || len(filtered.Items) != 1 {
		t.Fatalf("filter: %+v %v", filtered, err)
	}
	readExec(t, db, `UPDATE release_change_set SET version=2`)
	if _, err = e.Changes(ctx, ChangesRequest{Country: "vn"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("invalid set: %v", err)
	}
	readExec(t, db, `UPDATE release_change_set SET version=1`)
	readExec(t, db, `UPDATE release_change SET payload='{}' WHERE ordinal=1`)
	if _, err = e.Changes(ctx, ChangesRequest{Country: "vn"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("invalid row: %v", err)
	}
	readExec(t, db, `UPDATE release_change SET payload=$1 WHERE ordinal=1`, rows[0].Payload)
	all.Cursor = ""
	page, err := e.Changes(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	all.Cursor = page.NextCursor
	forged, err := decodeChangesCursor(page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	forged.LastOrdinal = 7
	all.Cursor = encodeChangesCursor(forged)
	if _, err = e.Changes(ctx, all); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("out-of-range ordinal: %v", err)
	}
	all.Cursor = page.NextCursor
	e.release.Rel = "20991111"
	_, err = e.Changes(ctx, all)
	if !errors.Is(err, ErrReleaseExpired) {
		t.Fatalf("release switch: %v", err)
	}
}

func TestChanges_States(t *testing.T) {
	db, e := readFixture(t)
	e.release.Country = "vn"
	e.release.SchemaVersion = 8
	ctx := context.Background()
	page, err := e.Changes(ctx, ChangesRequest{Country: "vn"})
	if err != nil || page.State != "unavailable" || len(page.Items) != 0 {
		t.Fatalf("old: %+v %v", page, err)
	}
	e.release.SchemaVersion = 9
	if _, err = e.Changes(ctx, ChangesRequest{Country: "vn"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing set: %v", err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = corpusdb.WriteReleaseChanges(ctx, tx, releasechanges.Header{Version: 1, Country: "vn", Release: e.release.Rel, Baseline: true}, nil); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	readExec(t, db, `UPDATE release_change_set SET predecessor_identity_hash=repeat('a',64)`)
	if _, err = e.Changes(ctx, ChangesRequest{Country: "vn"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("baseline with identity hash: %v", err)
	}
	readExec(t, db, `UPDATE release_change_set SET predecessor_identity_hash=NULL`)
	page, err = e.Changes(ctx, ChangesRequest{Country: "vn"})
	if err != nil || page.State != "baseline" || page.Counts != nil || len(page.Items) != 0 {
		t.Fatalf("baseline: %+v %v", page, err)
	}
	b, err := json.Marshal(page)
	if err != nil || !json.Valid(b) || !containsChangesItems(b) {
		t.Fatalf("JSON %s %v", b, err)
	}
}

func containsChangesItems(b []byte) bool {
	var v map[string]any
	if json.Unmarshal(b, &v) != nil {
		return false
	}
	return fmt.Sprint(v["items"]) == "[]"
}
