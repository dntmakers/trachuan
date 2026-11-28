package testpg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func MarkerRunKey(raw *string) string {
	if raw == nil {
		return ""
	}
	decoder := json.NewDecoder(strings.NewReader(*raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return ""
	}
	seen := make(map[string]bool)
	var runKey string
	for decoder.More() {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return ""
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return ""
		}
		seen[key] = true
		if key != "run_key" && key != "build_input_hash" && key != "build_recipe_hash" && key != "instance" {
			return ""
		}
		var rawValue json.RawMessage
		if decodeErr := decoder.Decode(&rawValue); decodeErr != nil || len(rawValue) == 0 || rawValue[0] != '"' {
			return ""
		}
		var value string
		if json.Unmarshal(rawValue, &value) != nil {
			return ""
		}
		if key == "run_key" {
			runKey = value
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return ""
	}
	if _, err := decoder.Token(); err != io.EOF || !validRunKey(runKey) {
		return ""
	}
	return runKey
}

type DatabaseMarker struct {
	OID    uint32
	Marker string
}

func sameMarkerIdentity(expected, actual DatabaseMarker) bool {
	return expected == actual
}

func DropIdleMarkedCorpus(ctx context.Context, conn *pgx.Conn, name string, expected DatabaseMarker) (bool, string, error) {
	if protectedPruneDatabase(name) {
		return false, "protected database", nil
	}
	if MarkerRunKey(&expected.Marker) == "" {
		return false, "invalid test marker", nil
	}
	var actual DatabaseMarker
	var marker *string
	var sessions int
	err := conn.QueryRow(ctx, `SELECT oid, pg_catalog.shobj_description(oid, 'pg_database'), (SELECT pg_catalog.count(*) FROM pg_catalog.pg_stat_activity WHERE datname=$1) FROM pg_catalog.pg_database WHERE datname=$1`, name).Scan(&actual.OID, &marker, &sessions)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "database missing", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("inspect marked corpus: %w", err)
	}
	if marker == nil {
		return false, "marker missing", nil
	}
	actual.Marker = *marker
	if !sameMarkerIdentity(expected, actual) {
		return false, "database identity or marker changed", nil
	}
	if sessions != 0 {
		return false, "connected sessions", nil
	}
	if err := dropOnce(ctx, conn, name); err != nil {
		return false, "", fmt.Errorf("drop marked corpus: %w", err)
	}
	return true, "", nil
}

type dropExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func dropOnce(ctx context.Context, conn dropExecutor, name string) error {
	_, err := conn.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize())
	return err
}

func protectedPruneDatabase(name string) bool {
	return strings.HasPrefix(name, "test_template_") || strings.HasPrefix(name, "corpus_tpl_")
}
