package corpusdb

import (
	"context"
	"errors"
	"fmt"

	"danny.vn/trachuan/internal/releasechanges"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type InvalidProjectionError struct{ Err error }

func (e *InvalidProjectionError) Error() string { return e.Err.Error() }
func (e *InvalidProjectionError) Unwrap() error { return e.Err }

func invalidProjection(format string, args ...any) error {
	return &InvalidProjectionError{Err: fmt.Errorf(format, args...)}
}

func ReleaseChangeLimits() ProjectionLimits {
	return ProjectionLimits{KeyCharacters: 512, PayloadBytes: 4096, TotalBytes: 128 * 1024 * 1024}
}

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
	_, err = WriteReleaseChangesStream(ctx, tx, header, func(emit func(releasechanges.Row) error) error {
		for _, row := range rows {
			if emitErr := emit(row); emitErr != nil {
				return emitErr
			}
		}
		return nil
	})
	return err
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

type ProjectionResult struct {
	Header releasechanges.Header
	Digest string
	Counts map[string]int
}

type projectionBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func ReadReleaseChanges(ctx context.Context, db projectionBeginner, limits ProjectionLimits, visit func(releasechanges.Row) error) (ProjectionResult, error) {
	var header releasechanges.Header
	if limits.KeyCharacters <= 0 || limits.PayloadBytes <= 0 || limits.TotalBytes <= 0 {
		return ProjectionResult{}, invalidProjection("invalid projection limits")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ProjectionResult{}, fmt.Errorf("begin projection snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var counts [6]int
	var digest *string
	var country, release *string
	var setCount int
	var headerOK bool
	err = tx.QueryRow(ctx, `SELECT version,CASE WHEN ok THEN country END,CASE WHEN ok THEN release END,CASE WHEN ok THEN predecessor END,CASE WHEN ok THEN predecessor_identity_hash END,baseline,document_added,document_removed,document_validity,provision_validity,provision_text,document_key,CASE WHEN ok THEN digest END,count(*) OVER (),ok FROM (SELECT *, (octet_length(country) <= 2 AND octet_length(release) <= 11 AND coalesce(octet_length(predecessor),0) <= 11 AND coalesce(octet_length(predecessor_identity_hash),0) <= 64 AND octet_length(digest) <= 64 AND 8::bigint + octet_length(version::text) + octet_length(country) + octet_length(release) + coalesce(octet_length(predecessor),0) + coalesce(octet_length(predecessor_identity_hash),0) <= $1) AS ok FROM release_change_set) bounded`, limits.TotalBytes).Scan(&header.Version, &country, &release, &header.Predecessor, &header.PredecessorIdentityHash, &header.Baseline, &counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &digest, &setCount, &headerOK)
	if err != nil {
		var scanErr pgx.ScanArgError
		var pgErr *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || errors.As(err, &scanErr) || (errors.As(err, &pgErr) && pgErr.Code == "42P01") {
			return ProjectionResult{}, invalidProjection("read projection set: %w", err)
		}
		return ProjectionResult{}, fmt.Errorf("read projection set: %w", err)
	}
	if setCount != 1 {
		return ProjectionResult{}, invalidProjection("invalid projection set count: %d", setCount)
	}
	if !headerOK || country == nil || release == nil || digest == nil {
		return ProjectionResult{}, invalidProjection("projection header exceeds limits")
	}
	header.Country, header.Release = *country, *release
	h, err := releasechanges.NewHasher(header)
	if err != nil {
		return ProjectionResult{}, invalidProjection("invalid projection header: %w", err)
	}
	headerBytes := int64(len(fmt.Sprintf("v%d\t%s\t%s\t%s\t%s\t%d\n", header.Version, header.Country, header.Release, optionalString(header.Predecessor), optionalString(header.PredecessorIdentityHash), boolInt(header.Baseline))))
	if headerBytes > limits.TotalBytes {
		return ProjectionResult{}, invalidProjection("projection exceeds total limit")
	}
	result, err := tx.Query(ctx, `SELECT ordinal, kind, CASE WHEN ok THEN key END, CASE WHEN ok THEN key2 END, CASE WHEN ok THEN payload END, ok FROM (SELECT *, (coalesce(char_length(key),0) <= $1 AND coalesce(char_length(key2),0) <= $1 AND octet_length(payload) <= $2 AND $3::numeric + sum(octet_length(ordinal::text)::numeric + octet_length(kind) + coalesce(octet_length(key),0) + coalesce(octet_length(key2),0) + octet_length(payload) + 5) OVER (ORDER BY ordinal) <= $4) AS ok FROM release_change) bounded ORDER BY ordinal`, limits.KeyCharacters, limits.PayloadBytes, headerBytes, limits.TotalBytes)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return ProjectionResult{}, invalidProjection("query projection: %w", err)
		}
		return ProjectionResult{}, fmt.Errorf("query projection: %w", err)
	}
	defer result.Close()
	for result.Next() {
		var row releasechanges.Row
		var payload *string
		var ok bool
		if err := result.Scan(&row.Ordinal, &row.Kind, &row.Key, &row.Key2, &payload, &ok); err != nil {
			return ProjectionResult{}, invalidProjection("read projection row: %w", err)
		}
		if !ok || payload == nil {
			return ProjectionResult{}, invalidProjection("projection exceeds limits")
		}
		row.Payload = *payload
		if err := h.Add(row); err != nil {
			return ProjectionResult{}, invalidProjection("invalid projection row: %w", err)
		}
		if visit != nil {
			if err := visit(row); err != nil {
				return ProjectionResult{}, fmt.Errorf("visit projection row: %w", err)
			}
		}
	}
	if err := result.Err(); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return ProjectionResult{}, invalidProjection("stream projection: %w", err)
		}
		return ProjectionResult{}, fmt.Errorf("stream projection: %w", err)
	}
	actual := h.Counts()
	for i, kind := range []string{"document_added", "document_removed", "document_validity", "provision_validity", "provision_text", "document_key"} {
		if counts[i] != actual[kind] {
			return ProjectionResult{}, invalidProjection("projection count mismatch: %s", kind)
		}
	}
	if *digest != h.Sum() {
		return ProjectionResult{}, invalidProjection("projection digest mismatch")
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectionResult{}, fmt.Errorf("finish projection snapshot: %w", err)
	}
	return ProjectionResult{Header: header, Digest: *digest, Counts: actual}, nil
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
