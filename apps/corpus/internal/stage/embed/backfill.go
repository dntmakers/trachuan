package embed

import (
	"context"
	"fmt"
	"io"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"github.com/jackc/pgx/v5"
)

type backfillDB interface {
	store.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

func (s *Stage) Backfill(ctx context.Context, env stage.Env, out io.Writer) error {
	recipe := s.Recipe(stage.Snapshot{})
	return s.backfill(ctx, env.Artifacts, env.DB, recipe, func(row store.LookupEmbedRowsRow) cachedArtifact {
		return s.openCached(env.Artifacts, recipe, row)
	}, out)
}

func (s *Stage) backfill(ctx context.Context, artifacts artifact.Store, db backfillDB, recipe string, open func(store.LookupEmbedRowsRow) cachedArtifact, out io.Writer) error {
	records, err := store.New(db).ListEmbedArtifacts(ctx, recipe)
	if err != nil {
		return fmt.Errorf("list embed artifacts: %w", err)
	}
	verified, skipped := 0, 0
	var total rowWriteCounts
	openIncumbent := memoizedIncumbentOpener(open)
	for _, record := range records {
		idx, candidateErr := s.openCandidate(artifacts, recipe, artifact.Result{InputHash: record.InputHash, Path: record.Path, SHA256: record.Sha256})
		if candidateErr != nil {
			skipped++
			_, _ = fmt.Fprintf(out, "artifact %d skipped: %v\n", record.ID, candidateErr)
			continue
		}
		verified++
		tx, beginErr := db.Begin(ctx)
		if beginErr != nil {
			return fmt.Errorf("begin embed index: %w", beginErr)
		}
		counts, indexErr := indexCandidate(ctx, store.New(tx), recipe, record.ID, idx, openIncumbent)
		if indexErr != nil {
			_ = tx.Rollback(ctx)
			return indexErr
		}
		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit embed index: %w", err)
		}
		total.inserted += counts.inserted
		total.repaired += counts.repaired
	}
	_, _ = fmt.Fprintf(out, "artifacts verified=%d rows inserted=%d rows repaired=%d artifacts skipped=%d\n", verified, total.inserted, total.repaired, skipped)
	return nil
}

type incumbentArtifactKey struct {
	id                                   int64
	stage, recipe, input, path, manifest string
}

func memoizedIncumbentOpener(open func(store.LookupEmbedRowsRow) cachedArtifact) func(store.LookupEmbedRowsRow) cachedArtifact {
	checked := make(map[incumbentArtifactKey]cachedArtifact)
	return func(row store.LookupEmbedRowsRow) cachedArtifact {
		key := incumbentArtifactKey{row.ArtifactID, row.ArtifactStage, row.ArtifactRecipeHash, row.InputHash, row.Path, row.Sha256}
		if old, ok := checked[key]; ok {
			return old
		}
		a := open(row)
		entry := cachedArtifact{index: Index{Rows: a.index.Rows}, err: a.err, stale: a.stale}
		checked[key] = entry
		return entry
	}
}
