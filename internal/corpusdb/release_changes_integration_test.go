//go:build integration

package corpusdb

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
)

func TestReleaseChanges_StreamWriter(t *testing.T) {
	ctx := context.Background()
	conn := schemaDB(t)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	header := releasechanges.Header{Version: 1, Country: "vn", Release: "r"}
	written, err := WriteReleaseChangesStream(ctx, tx, header, func(emit func(releasechanges.Row) error) error {
		return emit(releasechanges.Row{Ordinal: 1, Kind: "document_added", Payload: `{}`})
	})
	if err != nil || written.Counts["document_added"] != 1 {
		t.Fatalf("written=%+v err=%v", written, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	read, err := ReadReleaseChanges(ctx, conn, ProjectionLimits{KeyCharacters: 100, PayloadBytes: 1000, TotalBytes: 10000}, nil)
	if err != nil || !reflect.DeepEqual(read, written) {
		t.Fatalf("read=%+v written=%+v err=%v", read, written, err)
	}
}

func TestReleaseChanges_GoldenAndBaseline(t *testing.T) {
	ctx := context.Background()
	for _, baseline := range []bool{false, true} {
		t.Run(map[bool]string{false: "golden", true: "baseline"}[baseline], func(t *testing.T) {
			conn := schemaDB(t)
			header := releasechanges.Header{Version: 1, Country: "vn", Release: "r", Baseline: baseline}
			key := "vn:key"
			payload, err := releasechanges.CanonicalPayload([]byte(`{"z":null,"s":"\"\\\u0001Việt","n":-2,"zero":0}`))
			if err != nil {
				t.Fatal(err)
			}
			rows := []releasechanges.Row{{Ordinal: 1, Kind: "document_added", Key: &key, Payload: payload}}
			if baseline {
				rows = nil
			}
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := WriteReleaseChanges(ctx, tx, header, rows); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			var got []releasechanges.Row
			projection, err := ReadReleaseChanges(ctx, conn, ProjectionLimits{KeyCharacters: 100, PayloadBytes: 1000, TotalBytes: 10000}, func(row releasechanges.Row) error { got = append(got, row); return nil })
			if err != nil || !reflect.DeepEqual(got, rows) {
				t.Fatalf("rows %v: %v", got, err)
			}
			if !reflect.DeepEqual(projection.Header, header) || projection.Counts["document_added"] != len(rows) {
				t.Fatalf("projection = %+v", projection)
			}
			if !baseline {
				want, err := os.ReadFile("../releasechanges/testdata/projection.sha256")
				if err != nil {
					t.Fatal(err)
				}
				if projection.Digest != strings.TrimSpace(string(want)) {
					t.Fatalf("digest %s", projection.Digest)
				}
			}
		})
	}
}

func TestReleaseChanges_RejectsCorruption(t *testing.T) {
	ctx := context.Background()
	for name, query := range map[string]string{
		"payload":               `UPDATE release_change SET payload='{"x":2}'`,
		"deleted":               `DELETE FROM release_change`,
		"missing set":           `DELETE FROM release_change_set`,
		"unknown version":       `UPDATE release_change_set SET version=2`,
		"gap":                   `UPDATE release_change SET ordinal=2`,
		"duplicate set":         `ALTER TABLE release_change_set DROP CONSTRAINT release_change_set_pkey; ALTER TABLE release_change_set DROP CONSTRAINT release_change_set_singleton_check; INSERT INTO release_change_set SELECT false,version,country,release,predecessor,predecessor_identity_hash,baseline,document_added,document_removed,document_validity,provision_validity,provision_text,document_key,digest FROM release_change_set`,
		"oversize release":      `ALTER TABLE release_change_set DROP CONSTRAINT release_change_set_release_length; UPDATE release_change_set SET release=repeat('a',10000)`,
		"oversize country":      `ALTER TABLE release_change_set DROP CONSTRAINT release_change_set_country_length; UPDATE release_change_set SET country=repeat('a',10000)`,
		"oversize predecessor":  `ALTER TABLE release_change_set DROP CONSTRAINT release_change_set_predecessor_length; UPDATE release_change_set SET predecessor=repeat('a',10000)`,
		"oversize key":          `UPDATE release_change SET key=repeat('a',101)`,
		"oversize payload":      `UPDATE release_change SET payload='{"x":"' || repeat('a',1001) || '"}'`,
		"oversize total":        `UPDATE release_change SET payload='{"x":"' || repeat('a',100) || '"}'`,
		"oversize header total": `SELECT 1`,
	} {
		t.Run(name, func(t *testing.T) {
			conn := schemaDB(t)
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			key := "vn:key"
			header := releasechanges.Header{Version: 1, Country: "vn", Release: "r"}
			if err := WriteReleaseChanges(ctx, tx, header, []releasechanges.Row{{Ordinal: 1, Kind: "document_added", Key: &key, Payload: `{"x":1}`}}); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(ctx, query); err != nil {
				t.Fatal(err)
			}
			limits := ProjectionLimits{KeyCharacters: 100, PayloadBytes: 1000, TotalBytes: 10000}
			if name == "oversize total" {
				limits.TotalBytes = 100
			} else if name == "oversize header total" {
				limits.TotalBytes = 5
			}
			_, err = ReadReleaseChanges(ctx, conn, limits, nil)
			if err == nil {
				t.Fatal("accepted corruption")
			}
			var invalid *InvalidProjectionError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected invalid projection error, got %v", err)
			}
			if strings.HasPrefix(name, "oversize") && !strings.Contains(err.Error(), "exceeds limits") {
				t.Fatalf("expected limit error, got %v", err)
			}
		})
	}
}

func TestReleaseChanges_Constraints(t *testing.T) {
	ctx := context.Background()
	conn := schemaDB(t)
	cases := map[string]string{
		"ordinal":              `INSERT INTO release_change VALUES (0,'document_added',NULL,NULL,'{}')`,
		"kind":                 `INSERT INTO release_change VALUES (1,'bad',NULL,NULL,'{}')`,
		"empty key":            `INSERT INTO release_change VALUES (1,'document_added','',NULL,'{}')`,
		"tab key":              `INSERT INTO release_change VALUES (1,'document_added',E'a\tb',NULL,'{}')`,
		"empty key2":           `INSERT INTO release_change VALUES (1,'document_added',NULL,'','{}')`,
		"array":                `INSERT INTO release_change VALUES (1,'document_added',NULL,NULL,'[]')`,
		"duplicate JSON key":   `INSERT INTO release_change VALUES (1,'document_added',NULL,NULL,'{"x":1,"x":2}')`,
		"newline payload":      `INSERT INTO release_change VALUES (1,'document_added',NULL,NULL,E'{\n}')`,
		"oversize country":     `INSERT INTO release_change_set VALUES (true,1,'vnn','r',NULL,NULL,false,0,0,0,0,0,0,repeat('a',64))`,
		"oversize release":     `INSERT INTO release_change_set VALUES (true,1,'vn',repeat('a',12),NULL,NULL,false,0,0,0,0,0,0,repeat('a',64))`,
		"oversize predecessor": `INSERT INTO release_change_set VALUES (true,1,'vn','r',repeat('a',12),NULL,false,0,0,0,0,0,0,repeat('a',64))`,
		"bad digest":           `INSERT INTO release_change_set VALUES (true,1,'vn','r',NULL,NULL,false,0,0,0,0,0,0,'bad')`,
		"bad predecessor hash": `INSERT INTO release_change_set VALUES (true,1,'vn','r',NULL,'BAD',false,0,0,0,0,0,0,repeat('a',64))`,
		"baseline count":       `INSERT INTO release_change_set VALUES (true,1,'vn','r',NULL,NULL,true,1,0,0,0,0,0,repeat('a',64))`,
	}
	for name, query := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := conn.Exec(ctx, query); err == nil {
				t.Fatal("accepted invalid row")
			}
		})
	}
}
