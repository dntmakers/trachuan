//go:build integration

package build

import (
	"context"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/corpusdb"
	"github.com/jackc/pgx/v5"
)

func TestBuildProjection_ArtifactReuseChecksProjection(t *testing.T) {
	ctx := context.Background()
	f := recoverySetup(t)
	database, err := EnsureDatabase(ctx, f.cfg, f.env.Artifacts, f.art, f.run)
	if err != nil {
		t.Fatal(err)
	}
	cfg := f.admin.Config().Copy()
	cfg.Database = database.Name
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `DELETE FROM release_change_set`); err != nil {
		t.Fatal(err)
	}
	if err = conn.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = EnsureDatabase(ctx, f.cfg, f.env.Artifacts, f.art, f.run); err == nil {
		t.Fatal("artifact reuse accepted a missing projection")
	}
}

func TestBuildProjection_BaselineAndValidation(t *testing.T) {
	ctx := context.Background()
	f := recoverySetup(t)
	record, _, err := verifyBuildArtifact(f.env.Artifacts, f.art, f.run, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	name := "corpus_tmp_" + f.run.Country + "_" + f.item.InputHash[:24]
	cfg := f.admin.Config().Copy()
	cfg.Database = name
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	stored, err := corpusdb.ReadReleaseChanges(ctx, conn, corpusdb.ReleaseChangeLimits(), nil)
	if err != nil || !stored.Header.Baseline || stored.Digest != record.ChangesProjection.SHA256 || stored.Counts["document_added"] != 0 {
		t.Fatalf("stored=%+v record=%+v error=%v", stored, record.ChangesProjection, err)
	}
	if err = validateDatabase(ctx, f.cfg, name, record); err != nil {
		t.Fatal(err)
	}
	t.Run("record digest", func(t *testing.T) {
		bad := record
		bad.ChangesProjection.SHA256 = strings.Repeat("0", 64)
		if err := validateDatabase(ctx, f.cfg, name, bad); err == nil {
			t.Fatal("accepted incorrect projection digest")
		}
	})
	t.Run("tampered row", func(t *testing.T) {
		if _, err := conn.Exec(ctx, `INSERT INTO release_change VALUES (1,'document_added',NULL,NULL,'{}')`); err != nil {
			t.Fatal(err)
		}
		if err := validateDatabase(ctx, f.cfg, name, record); err == nil {
			t.Fatal("accepted tampered projection")
		}
		if _, err := conn.Exec(ctx, `DELETE FROM release_change`); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("missing set", func(t *testing.T) {
		if _, err := conn.Exec(ctx, `DELETE FROM release_change_set`); err != nil {
			t.Fatal(err)
		}
		if err := validateDatabase(ctx, f.cfg, name, record); err == nil {
			t.Fatal("accepted missing projection")
		}
	})
}
