package embed

import (
	"context"
	"fmt"
	"sort"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Stage) Rows(_ context.Context, env stage.Env, art artifact.Result) (func(context.Context, pgx.Tx, int64) error, error) {
	return func(ctx context.Context, tx pgx.Tx, id int64) error {
		recipe := s.Recipe(stage.Snapshot{})
		idx, err := s.openCandidate(env.Artifacts, recipe, art)
		if err != nil {
			return err
		}
		q := store.New(tx)
		match, err := q.MatchEmbedArtifact(ctx, store.MatchEmbedArtifactParams{ID: id, RecipeHash: recipe, InputHash: art.InputHash, Path: art.Path, Sha256: art.SHA256})
		if err != nil {
			return fmt.Errorf("check embed artifact record: %w", err)
		}
		if !match {
			return fmt.Errorf("embed artifact record %d does not match candidate", id)
		}
		_, err = indexCandidate(ctx, q, recipe, id, idx, func(row store.LookupEmbedRowsRow) cachedArtifact {
			return s.openCached(env.Artifacts, recipe, row)
		})
		return err
	}, nil
}

func (s *Stage) openCandidate(artifacts artifact.Store, recipe string, art artifact.Result) (Index, error) {
	candidate := s.openRecorded(artifacts, recipe, art)
	if candidate.err != nil {
		return Index{}, candidate.err
	}
	idx := candidate.index
	report, err := readReport(artifacts.Abs(art.Path))
	if err != nil {
		return Index{}, err
	}
	if report.Rows != len(idx.Rows) || report.Rejected != len(idx.Rejected) {
		return Index{}, fmt.Errorf("embedding report counts mismatch")
	}
	return idx, nil
}

type rowWriteCounts struct{ inserted, repaired int64 }

type incumbentRow struct {
	row   store.LookupEmbedRowsRow
	stale bool
}

func indexCandidate(ctx context.Context, q *store.Queries, recipe string, id int64, idx Index, open func(store.LookupEmbedRowsRow) cachedArtifact) (rowWriteCounts, error) {
	if len(idx.Rows) == 0 {
		return rowWriteCounts{}, nil
	}
	hashes := make([]string, 0, len(idx.Rows))
	for _, row := range idx.Rows {
		hashes = append(hashes, row.InputSHA256)
	}
	refs, err := q.LookupEmbedRows(ctx, store.LookupEmbedRowsParams{RecipeHash: recipe, InputHashes: hashes})
	if err != nil {
		return rowWriteCounts{}, fmt.Errorf("lookup existing embed rows: %w", err)
	}
	incumbents := make(map[string]incumbentRow, len(refs))
	visitCachedRows(refs, func(row store.LookupEmbedRowsRow) cachedArtifact {
		if row.ArtifactID == id {
			return cachedArtifact{index: idx}
		}
		return open(row)
	}, func(row store.LookupEmbedRowsRow, a cachedArtifact, reason error) {
		incumbents[row.InputSha256] = incumbentRow{row, reason != nil && (a.err == nil || a.stale)}
	})
	return writeRows(ctx, q, recipe, id, idx.Rows, incumbents)
}

func writeRows(ctx context.Context, q *store.Queries, recipe string, id int64, rows []Row, incumbents map[string]incumbentRow) (rowWriteCounts, error) {
	var counts rowWriteCounts
	ordered := append([]Row(nil), rows...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].InputSHA256 < ordered[j].InputSHA256 })
	for i, row := range ordered {
		if i > 0 && ordered[i-1].InputSHA256 == row.InputSHA256 {
			continue
		}
		old, exists := incumbents[row.InputSHA256]
		if !exists {
			n, err := q.InsertEmbedRow(ctx, store.InsertEmbedRowParams{RecipeHash: recipe, InputSha256: row.InputSHA256, ArtifactID: id, Row: int32(row.Row), TokenCount: int32(row.TokenCount)})
			if err != nil {
				return counts, fmt.Errorf("insert embed row: %w", err)
			}
			counts.inserted += n
		} else if old.stale {
			n, err := q.RepairEmbedRow(ctx, store.RepairEmbedRowParams{
				RecipeHash: recipe, InputSha256: row.InputSHA256, ArtifactID: id, Row: int32(row.Row), TokenCount: int32(row.TokenCount),
				OldArtifactID: old.row.ArtifactID, OldRow: old.row.Row, OldTokenCount: old.row.TokenCount,
			})
			if err != nil {
				return counts, fmt.Errorf("repair embed row: %w", err)
			}
			counts.repaired += n
		}
	}
	return counts, nil
}
