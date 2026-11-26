//go:build integration

package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
)

func TestChanges_BytePagingAndIndexSeeks(t *testing.T) {
	db, e := readFixture(t)
	e.release.Country = "vn"
	e.release.SchemaVersion = 9
	ctx := context.Background()
	previous := "20991109"
	hash := strings.Repeat("a", 64)
	key := "vn:1"
	old := "vn:old"
	rows := make([]releasechanges.Row, 0, 3001)
	for i := 1; i <= 3000; i++ {
		itemKey := fmt.Sprintf("vn:%04d%s", i, strings.Repeat("x", 490))
		payload := fmt.Sprintf(`{"ambiguous":false,"anchor":%q,"change":"added","key":%q,"provenance":false}`, strings.Repeat("a", 500), itemKey)
		rows = append(rows, releasechanges.Row{Ordinal: int64(i), Kind: "provision_text", Key: &itemKey, Payload: payload})
	}
	rows = append(rows, releasechanges.Row{Ordinal: 3001, Kind: "document_key", Key: &key, Key2: &old, Payload: `{"before_key":"vn:old","key":"vn:1"}`})
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
	readExec(t, db, `ANALYZE release_change`)
	for _, tc := range []struct {
		name, query, index string
		args               []any
	}{
		{"kind", changesSeekKind, "release_change_kind_ordinal_idx", []any{"document_key", int64(0), 21}},
		{"key", changesSeekKey, "release_change_kind_key_ordinal_idx", []any{"provision_text", fmt.Sprintf("vn:%04d%s", 1, strings.Repeat("x", 490)), int64(0), 21}},
		{"key2", changesSeekKey2, "release_change_kind_key2_ordinal_idx", []any{"document_key", old, int64(0), 21}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := db.Query(ctx, "EXPLAIN "+tc.query, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			defer result.Close()
			var lines []string
			for result.Next() {
				var line string
				if err = result.Scan(&line); err != nil {
					t.Fatal(err)
				}
				lines = append(lines, line)
			}
			if err = result.Err(); err != nil {
				t.Fatal(err)
			}
			plan := strings.Join(lines, "\n")
			t.Log(plan)
			if !strings.Contains(plan, tc.index) || strings.Contains(plan, "Seq Scan") {
				t.Fatalf("expected index seek: %s", plan)
			}
		})
	}
	rare, err := e.Changes(ctx, ChangesRequest{Country: "vn", Kinds: []string{"document_key"}, Document: old})
	if err != nil || len(rare.Items) != 1 {
		t.Fatalf("rare: %+v %v", rare, err)
	}
	empty, err := e.Changes(ctx, ChangesRequest{Country: "vn", Document: "vn:missing"})
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("empty: %+v %v", empty, err)
	}
	limit := 50
	req := ChangesRequest{Country: "vn", Kinds: []string{"provision_text"}, Limit: &limit}
	count := 0
	for count < 3000 {
		page, err := e.Changes(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(page)
		if err != nil || len(raw) > 32768 || len(page.Items) == 0 {
			t.Fatalf("page %d bytes, items %d: %v", len(raw), len(page.Items), err)
		}
		for _, item := range page.Items {
			var value struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(item, &value); err != nil || !strings.HasPrefix(value.Key, fmt.Sprintf("vn:%04d", count+1)) {
				t.Fatalf("unexpected item %d: %v", count+1, err)
			}
			count++
		}
		if count < 3000 && page.NextCursor == "" {
			t.Fatal("lost continuation")
		}
		if count == 3000 && page.NextCursor != "" {
			t.Fatal("extra continuation")
		}
		req.Cursor = page.NextCursor
	}
	if count != 3000 {
		t.Fatalf("count %d", count)
	}
}
