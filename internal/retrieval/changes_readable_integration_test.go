//go:build integration

package retrieval

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
)

func TestChanges_Schema8WithoutProjectionTables(t *testing.T) {
	db, e := readFixture(t)
	readExec(t, db, `DROP TABLE release_change, release_change_set`)
	e.release.SchemaVersion = 8
	result, err := e.Changes(context.Background(), ChangesRequest{Country: "vn"})
	if err != nil || result.State != "unavailable" || len(result.Items) != 0 {
		t.Fatalf("schema 8: %+v %v", result, err)
	}
}

func TestChanges_HandlesRequireReadableIdentity(t *testing.T) {
	db, e := readFixture(t)
	e.release.SchemaVersion = 9
	e.release.Country = "vn"
	old := "20991109"
	hash := strings.Repeat("a", 64)
	present, missing := "vn:1", "vn:missing"
	rows := []releasechanges.Row{
		{Ordinal: 1, Kind: "document_added", Key: &present, Payload: `{"cause":"membership","key":"vn:1"}`},
		{Ordinal: 2, Kind: "document_added", Key: &missing, Payload: `{"cause":"membership","key":"vn:missing"}`},
		{Ordinal: 3, Kind: "provision_text", Key: &present, Payload: `{"ambiguous":false,"anchor":"missing","change":"added","key":"vn:1","provenance":false}`},
		{Ordinal: 4, Kind: "provision_text", Key: &present, Payload: `{"ambiguous":true,"anchor":"p5","change":"added","key":"vn:1","provenance":false}`},
		{Ordinal: 5, Kind: "provision_text", Key: &present, Payload: `{"ambiguous":false,"anchor":"p5","change":"added","key":"vn:1","provenance":false}`},
	}
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = corpusdb.WriteReleaseChanges(ctx, tx, releasechanges.Header{Version: releasechanges.Version, Country: "vn", Release: e.release.Rel, Predecessor: &old, PredecessorIdentityHash: &hash}, rows); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := e.Changes(ctx, ChangesRequest{Country: "vn", Kinds: []string{"document_added", "provision_text"}})
	if err != nil || len(result.Items) != len(rows) {
		t.Fatalf("result: %+v %v", result, err)
	}
	expected := []string{"vn:1", "", "", "", "vn:1#p5"}
	for i, item := range result.Items {
		var value struct {
			Handle string `json:"handle"`
		}
		if err = json.Unmarshal(item, &value); err != nil || value.Handle != expected[i] {
			t.Fatalf("item %d: %s %v", i, item, err)
		}
	}
}
