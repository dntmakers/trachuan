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
	_, err = tx.Exec(ctx, `INSERT INTO release_change_set (version,country,release,predecessor,predecessor_identity_hash,baseline,document_added,document_removed,document_validity,provision_validity,provision_text,document_key,digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, header.Version, header.Country, header.Release, header.Predecessor, header.PredecessorIdentityHash, header.Baseline, counts["document_added"], counts["document_removed"], counts["document_validity"], counts["provision_validity"], counts["provision_text"], counts["document_key"], h.Sum())
	if err != nil {
		return ProjectionResult{}, fmt.Errorf("insert projection set: %w", err)
	}
	return ProjectionResult{Header: header, Digest: h.Sum(), Counts: counts}, nil
}
