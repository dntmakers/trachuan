//go:build integration

package testpg

import (
	"context"
	"errors"
	"fmt"

	"danny.vn/trachuan/internal/pgdrop"
	"github.com/jackc/pgx/v5"
)

// CreateCorpus reserves and creates a marked test corpus database.
func CreateCorpus(ctx context.Context, conn *pgx.Conn, seed string) (string, string, error) {
	rel, _, err := acquireReservation(ctx, conn, seed)
	if err != nil {
		return "", "", fmt.Errorf("reserve test release: %w", err)
	}
	name, runKey := "corpus_vn_"+rel, "vn/"+rel
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return "", "", fmt.Errorf("create test corpus: %w", err)
	}
	marker := `{"run_key":"` + runKey + `"}`
	if _, err := conn.Exec(ctx, "COMMENT ON DATABASE "+pgx.Identifier{name}.Sanitize()+" IS '"+marker+"'"); err != nil {
		return name, runKey, fmt.Errorf("mark test corpus: %w", err)
	}
	return name, runKey, nil
}

func DropMarkedCorpus(ctx context.Context, conn *pgx.Conn, name, runKey string) (bool, error) {
	marked, err := markedCorpus(ctx, conn, name, runKey)
	if err != nil || !marked {
		return false, err
	}
	if err := pgdrop.Drop(ctx, conn, name); err != nil {
		return false, fmt.Errorf("drop marked corpus: %w", err)
	}
	return true, nil
}

func markedCorpus(ctx context.Context, conn *pgx.Conn, name, runKey string) (bool, error) {
	if !validRunKey(runKey) {
		return false, nil
	}
	var raw *string
	err := conn.QueryRow(ctx, `SELECT pg_catalog.shobj_description(oid, 'pg_database') FROM pg_catalog.pg_database WHERE datname=$1`, name).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read database marker: %w", err)
	}
	return MarkerRunKey(raw) == runKey, nil
}
