package testdb

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5"
)

func testDatabaseName(name string) bool {
	return strings.HasPrefix(name, "corpus_tmp_vn_") || regexp.MustCompile(`^(corpus_vn_|frameworks_)2099[0-9]{4}(_[0-9]{1,2})?$`).MatchString(name)
}

func pruneTestDatabases(ctx context.Context, admin *pgx.Conn, out io.Writer) error {
	rows, err := admin.Query(ctx, `SELECT datname, oid, pg_catalog.shobj_description(oid, 'pg_database') FROM pg_catalog.pg_database ORDER BY datname`)
	if err != nil {
		return fmt.Errorf("list test databases: %w", err)
	}
	type candidate struct {
		name, key string
		marker    testpg.DatabaseMarker
	}
	var candidates []candidate
	for rows.Next() {
		var name string
		var oid uint32
		var marker *string
		if scanErr := rows.Scan(&name, &oid, &marker); scanErr != nil {
			rows.Close()
			return fmt.Errorf("read database marker: %w", scanErr)
		}
		key := testpg.MarkerRunKey(marker)
		if key != "" || testDatabaseName(name) {
			var raw string
			if marker != nil {
				raw = *marker
			}
			candidates = append(candidates, candidate{name, key, testpg.DatabaseMarker{OID: oid, Marker: raw}})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("list test databases: %w", err)
	}
	for _, c := range candidates {
		if strings.HasPrefix(c.name, "corpus_tpl_") {
			continue
		}
		if strings.HasPrefix(c.name, "test_template_") {
			if _, err := fmt.Fprintf(out, "skipped %s: protected builder template\n", c.name); err != nil {
				return err
			}
			continue
		}
		if c.key == "" {
			if _, err := fmt.Fprintf(out, "skipped %s: missing test marker\n", c.name); err != nil {
				return err
			}
			continue
		}
		dropped, reason, err := testpg.DropIdleMarkedCorpus(ctx, admin, c.name, c.marker)
		if err != nil {
			return fmt.Errorf("drop %s: %w", c.name, err)
		}
		if !dropped {
			if _, err := fmt.Fprintf(out, "skipped %s: %s\n", c.name, reason); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(out, "dropped %s\n", c.name); err != nil {
			return err
		}
	}
	return nil
}

func listBuilderRoles(ctx context.Context, admin *pgx.Conn) ([]string, error) {
	rows, err := admin.Query(ctx, `SELECT rolname FROM pg_catalog.pg_roles WHERE rolname ~ '^test_builder_[0-9a-f]{24}$' ORDER BY rolname`)
	if err != nil {
		return nil, fmt.Errorf("list test builder roles: %w", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			rows.Close()
			return nil, fmt.Errorf("read builder role: %w", scanErr)
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("list builder roles: %w", err)
	}
	return names, nil
}

func pruneBuilderRoles(ctx context.Context, admin *pgx.Conn, out io.Writer, names []string) error {
	for _, name := range names {
		if err := pruneBuilderRole(ctx, admin, out, name); err != nil {
			return err
		}
	}
	return nil
}

func pruneBuilderRole(ctx context.Context, admin *pgx.Conn, out io.Writer, name string) error {
	var ownsDatabase, connected bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT FROM pg_catalog.pg_database d JOIN pg_catalog.pg_roles r ON r.oid=d.datdba WHERE r.rolname=$1), EXISTS (SELECT FROM pg_catalog.pg_stat_activity WHERE usename=$1)`, name).Scan(&ownsDatabase, &connected); err != nil {
		return fmt.Errorf("inspect builder role: %w", err)
	}
	if reason := builderRoleSkipReason(ownsDatabase, connected); reason != "" {
		_, err := fmt.Fprintf(out, "skipped role %s: %s\n", name, reason)
		return err
	}
	if _, err := admin.Exec(ctx, "DROP ROLE "+pgx.Identifier{name}.Sanitize()); err != nil {
		_, writeErr := fmt.Fprintf(out, "skipped role %s: %v\n", name, err)
		return writeErr
	}
	_, err := fmt.Fprintf(out, "dropped role %s\n", name)
	return err
}

func builderRoleSkipReason(ownsDatabase, connected bool) string {
	if ownsDatabase || connected {
		return "owns a database or has connected sessions"
	}
	return ""
}
