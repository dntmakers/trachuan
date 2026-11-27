package corpusdb

import (
	"context"
	"fmt"

	"danny.vn/trachuan/internal/releasechanges"
	"github.com/jackc/pgx/v5"
)

func WriteReleaseChangesStream(ctx context.Context, tx pgx.Tx, header releasechanges.Header, source func(func(releasechanges.Row) error) error) (ProjectionResult, error) {
	h, err := releasechanges.NewWriter(header, discardWriter{})
	if err != nil {
		return ProjectionResult{}, fmt.Errorf("projection header: %w", err)
	}
	err = source(func(row releasechanges.Row) error {
		if addErr := h.Add(row); addErr != nil {
			return fmt.Errorf("projection row: %w", addErr)
		}
		if _, insertErr := tx.Exec(ctx, `INSERT INTO release_change (ordinal,kind,key,key2,payload) VALUES ($1,$2,$3,$4,$5)`, row.Ordinal, row.Kind, row.Key, row.Key2, row.Payload); insertErr != nil {
			return fmt.Errorf("insert projection row: %w", insertErr)
		}
		return nil
	})
	if err != nil {
		return ProjectionResult{}, fmt.Errorf("write projection: %w", err)
	}
	counts := h.Counts()
	args := []any{header.Version, header.Country, header.Release, header.Predecessor, header.PredecessorIdentityHash, header.Baseline}
	placeholders := "$1,$2,$3,$4,$5,$6"
	for i, kind := range releasechanges.Kinds() {
		args = append(args, counts[kind])
		placeholders += fmt.Sprintf(",$%d", i+7)
	}
	args = append(args, h.Sum())
	placeholders += fmt.Sprintf(",$%d", len(args))
	_, err = tx.Exec(ctx, `INSERT INTO release_change_set (version,country,release,predecessor,predecessor_identity_hash,baseline,`+releasechanges.CountColumns()+`,digest) VALUES (`+placeholders+`)`, args...)
	if err != nil {
		return ProjectionResult{}, fmt.Errorf("insert projection set: %w", err)
	}
	return ProjectionResult{Header: header, Digest: h.Sum(), Counts: counts}, nil
}
