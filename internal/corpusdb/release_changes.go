package corpusdb

import (
	"context"
	"fmt"

	"danny.vn/trachuan/internal/releasechanges"
	"github.com/jackc/pgx/v5"
)

type ProjectionLimits struct {
	KeyCharacters int
	PayloadBytes  int
	TotalBytes    int64
}

func WriteReleaseChanges(ctx context.Context, tx pgx.Tx, header releasechanges.Header, rows []releasechanges.Row) error {
	h, err := releasechanges.NewWriter(header, discardWriter{})
	if err != nil {
		return fmt.Errorf("projection header: %w", err)
	}
	for _, row := range rows {
		if addErr := h.Add(row); addErr != nil {
			return fmt.Errorf("projection row: %w", addErr)
		}
	}
	counts := h.Counts()
	_, err = tx.Exec(ctx, `INSERT INTO release_change_set (version,country,release,predecessor,predecessor_identity_hash,baseline,document_added,document_removed,document_validity,provision_validity,provision_text,document_key,digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, header.Version, header.Country, header.Release, header.Predecessor, header.PredecessorIdentityHash, header.Baseline, counts["document_added"], counts["document_removed"], counts["document_validity"], counts["provision_validity"], counts["provision_text"], counts["document_key"], h.Sum())
	if err != nil {
		return fmt.Errorf("insert projection set: %w", err)
	}
	for _, row := range rows {
		if _, err := tx.Exec(ctx, `INSERT INTO release_change (ordinal,kind,key,key2,payload) VALUES ($1,$2,$3,$4,$5)`, row.Ordinal, row.Kind, row.Key, row.Key2, row.Payload); err != nil {
			return fmt.Errorf("insert projection row: %w", err)
		}
	}
	return nil
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

type projectionBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func ReadReleaseChanges(ctx context.Context, db projectionBeginner, limits ProjectionLimits, visit func(releasechanges.Row) error) (releasechanges.Header, error) {
	var header releasechanges.Header
	if limits.KeyCharacters <= 0 || limits.PayloadBytes <= 0 || limits.TotalBytes <= 0 {
		return header, fmt.Errorf("invalid projection limits")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return header, fmt.Errorf("begin projection snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var counts [6]int
	var digest string
	var setCount int
	err = tx.QueryRow(ctx, `SELECT version,country,release,predecessor,predecessor_identity_hash,baseline,document_added,document_removed,document_validity,provision_validity,provision_text,document_key,digest,count(*) OVER () FROM release_change_set`).Scan(&header.Version, &header.Country, &header.Release, &header.Predecessor, &header.PredecessorIdentityHash, &header.Baseline, &counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &digest, &setCount)
	if err != nil {
		return header, fmt.Errorf("read projection set: %w", err)
	}
	if setCount != 1 {
		return header, fmt.Errorf("invalid projection set count: %d", setCount)
	}
	h, err := releasechanges.NewHasher(header)
	if err != nil {
		return header, fmt.Errorf("invalid projection header: %w", err)
	}
	headerBytes := int64(len(fmt.Sprintf("v%d\t%s\t%s\t%s\t%s\t%d\n", header.Version, header.Country, header.Release, optionalString(header.Predecessor), optionalString(header.PredecessorIdentityHash), boolInt(header.Baseline))))
	if headerBytes > limits.TotalBytes {
		return header, fmt.Errorf("projection exceeds total limit")
	}
	result, err := tx.Query(ctx, `SELECT ordinal, kind, CASE WHEN ok THEN key END, CASE WHEN ok THEN key2 END, CASE WHEN ok THEN payload END, ok FROM (SELECT *, (coalesce(char_length(key),0) <= $1 AND coalesce(char_length(key2),0) <= $1 AND octet_length(payload) <= $2 AND $3::numeric + sum(octet_length(ordinal::text)::numeric + octet_length(kind) + coalesce(octet_length(key),0) + coalesce(octet_length(key2),0) + octet_length(payload) + 5) OVER (ORDER BY ordinal) <= $4) AS ok FROM release_change) bounded ORDER BY ordinal`, limits.KeyCharacters, limits.PayloadBytes, headerBytes, limits.TotalBytes)
	if err != nil {
		return header, fmt.Errorf("query projection: %w", err)
	}
	defer result.Close()
	for result.Next() {
		var row releasechanges.Row
		var payload *string
		var ok bool
		if err := result.Scan(&row.Ordinal, &row.Kind, &row.Key, &row.Key2, &payload, &ok); err != nil {
			return header, fmt.Errorf("read projection row: %w", err)
		}
		if !ok || payload == nil {
			return header, fmt.Errorf("projection exceeds limits")
		}
		row.Payload = *payload
		if err := h.Add(row); err != nil {
			return header, fmt.Errorf("invalid projection row: %w", err)
		}
		if visit != nil {
			if err := visit(row); err != nil {
				return header, fmt.Errorf("visit projection row: %w", err)
			}
		}
	}
	if err := result.Err(); err != nil {
		return header, fmt.Errorf("stream projection: %w", err)
	}
	actual := h.Counts()
	for i, kind := range []string{"document_added", "document_removed", "document_validity", "provision_validity", "provision_text", "document_key"} {
		if counts[i] != actual[kind] {
			return header, fmt.Errorf("projection count mismatch: %s", kind)
		}
	}
	if digest != h.Sum() {
		return header, fmt.Errorf("projection digest mismatch")
	}
	if err := tx.Commit(ctx); err != nil {
		return header, fmt.Errorf("finish projection snapshot: %w", err)
	}
	return header, nil
}
func optionalString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
