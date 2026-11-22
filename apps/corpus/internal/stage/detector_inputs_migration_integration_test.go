//go:build integration

package stage

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/testdb"
	"danny.vn/trachuan/apps/corpus/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestDetectorInputsMigration_SelectionMatrix(t *testing.T) {
	ctx := context.Background()
	cfg, err := pgx.ParseConfig(testdb.NewEmpty(t))
	if err != nil {
		t.Fatal(err)
	}
	db := stdlib.OpenDB(*cfg)
	defer func() { _ = db.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(ctx, 16); err != nil {
		t.Fatal(err)
	}
	var runID, textID, checkID int64
	if err = db.QueryRowContext(ctx, `INSERT INTO run(country,rel,state,snapshot) VALUES ('vn','20990101','running','{"Recipes":{"extract":"old"}}') RETURNING id`).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	if err = db.QueryRowContext(ctx, `INSERT INTO ocr_text(pdf_sha256,engine_id,text_sha256,bytes,origin) VALUES ($1,'engine',$1,1,'vision') RETURNING id`, digest).Scan(&textID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `INSERT INTO ocr_check(ocr_text_id,checker_id,result_sha256,result,flagged) VALUES ($1,'checker',$2,'{}',0) RETURNING id`, textID, digest).Scan(&checkID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO run_ocr_selection(run_id,item_key,gap) VALUES ($1,'old','ocr_rejected')`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(ctx, 17); err != nil {
		t.Fatal(err)
	}
	var version int
	var contract string
	if err = db.QueryRowContext(ctx, `SELECT s.selection_version, r.snapshot->>'extract_record_contract' FROM run_ocr_selection s JOIN run r ON r.id=s.run_id WHERE s.item_key='old'`).Scan(&version, &contract); err != nil || version != 0 || contract != "legacy" {
		t.Fatalf("legacy version=%d contract=%q err=%v", version, contract, err)
	}
	cases := []struct {
		state, source, gap string
		ids, valid         bool
	}{
		{"checked", "ocr", "", true, true},
		{"checked", "none", "ocr_rejected", true, true},
		{"checked", "none", "ocr_label_flagged", true, true},
		{"checked", "source_tree", "tree_headings_only", true, true},
		{"unavailable", "none", "needs_ocr", false, true},
		{"unavailable", "none", "text_source_ambiguous", false, true},
		{"unavailable", "source_tree", "tree_headings_only", false, true},
		{"not_selected", "none", "text_number_mismatch", false, true},
		{"not_selected", "source_tree", "tree_headings_only", false, true},
		{"checked", "ocr", "", false, false},
		{"checked", "none", "needs_ocr", true, false},
		{"unavailable", "ocr", "", false, false},
		{"not_selected", "none", "text_number_mismatch", true, false},
		{"unknown", "none", "needs_ocr", false, false},
	}
	for i, tc := range cases {
		var pdf, text, check any
		if tc.ids {
			pdf, text, check = digest, textID, checkID
		}
		_, err = db.ExecContext(ctx, `INSERT INTO run_ocr_selection(run_id,item_key,selection_version,selection_state,text_source,gap,fetch_sha256,pdf_sha256,ocr_text_id,ocr_check_id) VALUES ($1,$2,1,$3,$4,$5,$6,$7,$8,$9)`, runID, fmt.Sprint(i), tc.state, tc.source, tc.gap, digest, pdf, text, check)
		if (err == nil) != tc.valid {
			t.Errorf("case %+v: %v", tc, err)
		}
	}
	for _, query := range []string{
		`INSERT INTO run_ocr_selection(run_id,item_key,selection_version,selection_state,text_source,gap,fetch_sha256) VALUES ($1,'no-fetch',1,'unavailable','none','needs_ocr',NULLIF($2,$2))`,
		`INSERT INTO run_ocr_selection(run_id,item_key,selection_version,selection_state,text_source,gap,fetch_sha256) VALUES ($1,'no-state',1,NULL,'none','needs_ocr',$2)`,
		`INSERT INTO run_ocr_selection(run_id,item_key,selection_version,selection_state,text_source,gap,fetch_sha256,pdf_sha256) VALUES ($1,'partial',1,'checked','ocr','',$2,$2)`,
	} {
		_, err = db.ExecContext(ctx, query, runID, digest)
		if err == nil {
			t.Errorf("invalid selection accepted: %s", query)
		}
	}
}
