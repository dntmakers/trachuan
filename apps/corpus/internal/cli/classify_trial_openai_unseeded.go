package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"

	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/config"
	"danny.vn/trachuan/apps/corpus/internal/stage/build"
	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
	"github.com/jackc/pgx/v5"
)

func openAITrialLabels(ctx context.Context, opts openAITrialCLI) (taxonomy.Labels, string, error) {
	if opts.part == 0 {
		labels, hash, err := classify.ReadLabels(opts.labels)
		if err != nil {
			return labels, "", err
		}
		raw, err := os.ReadFile(opts.labels)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != hash {
			return labels, "", fmt.Errorf("trial labels changed while reading")
		}
		if _, err = classify.VerifyScoreLabels(opts.trial.Recipe, raw); err != nil {
			return labels, "", err
		}
		return labels, hash, nil
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return taxonomy.Labels{}, "", err
	}
	name, err := build.DatabaseName(opts.trial.Country, opts.trial.Release)
	if err != nil {
		return taxonomy.Labels{}, "", err
	}
	db, err := trialReadPool(ctx, cfg.BuilderDatabaseURL, name, "release")
	if err != nil {
		return taxonomy.Labels{}, "", err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return taxonomy.Labels{}, "", fmt.Errorf("open release selection snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var input, recipe string
	if err = tx.QueryRow(ctx, `SELECT build_input_hash,build_recipe_hash FROM release WHERE singleton AND country=$1 AND rel=$2`, opts.trial.Country, opts.trial.Release).Scan(&input, &recipe); err != nil {
		return taxonomy.Labels{}, "", fmt.Errorf("read trial release identity: %w", err)
	}
	labels, hash, err := classify.LoadUnseededSelection(ctx, tx, opts.trial.Country, opts.trial.Release, opts.part, opts.total)
	if err != nil {
		return labels, "", err
	}
	hash, err = classify.UnseededBuildHash(hash, input, recipe)
	if err != nil {
		return labels, "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return labels, "", fmt.Errorf("close release selection snapshot: %w", err)
	}
	return labels, hash, nil
}

func validateUnseededFamilies(hash string, families classify.FamilyArtifact) error {
	if families.Selection == nil || families.Selection.LabelsHash != hash {
		return fmt.Errorf("families artifact does not match unseeded selection")
	}
	return nil
}
