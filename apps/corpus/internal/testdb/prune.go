package testdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func pruneSkipReason(name, current string, sessions int) string {
	if !regexp.MustCompile(`^corpus_tpl_[0-9a-f]{12}$`).MatchString(name) {
		return "invalid name"
	}
	if name == current {
		return "current migration"
	}
	if sessions != 0 {
		return "connected sessions"
	}
	return ""
}

func Prune(ctx context.Context, out io.Writer) error {
	current, err := migrationTemplateName()
	if err != nil {
		return err
	}
	admin, err := testpg.OpenPruneAdministrator(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close(ctx) }()
	var superuser bool
	if scanErr := admin.QueryRow(ctx, `SELECT rolsuper FROM pg_catalog.pg_roles WHERE rolname=current_user`).Scan(&superuser); scanErr != nil {
		return fmt.Errorf("check test administrator privileges: %w", scanErr)
	}
	if !superuser {
		return fmt.Errorf("refusing to prune: test administrator must be a superuser")
	}
	if err = refuseActiveTests(ctx, admin); err != nil {
		return err
	}
	rows, err := admin.Query(ctx, `SELECT datname FROM pg_catalog.pg_database WHERE pg_catalog.left(datname, 11) = 'corpus_tpl_' ORDER BY datname`)
	if err != nil {
		return fmt.Errorf("list templates: %w", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("read template name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("list templates: %w", err)
	}
	rows.Close()
	for _, name := range names {
		if err := pruneOne(ctx, admin, out, name, current); err != nil {
			return err
		}
	}
	if err := pruneTestDatabases(ctx, admin, out); err != nil {
		return err
	}
	roles, listErr := listBuilderRoles(ctx, admin)
	if listErr != nil {
		return listErr
	}
	return pruneBuilderRoles(ctx, admin, out, roles)
}

func refuseActiveTests(ctx context.Context, admin *pgx.Conn) error {
	rows, err := admin.Query(ctx, `SELECT DISTINCT d.datname, pg_catalog.shobj_description(d.oid, 'pg_database') FROM pg_catalog.pg_database d JOIN pg_catalog.pg_stat_activity a ON a.datid=d.oid ORDER BY d.datname`)
	if err != nil {
		return fmt.Errorf("inspect active databases: %w", err)
	}
	defer rows.Close()
	var active []string
	for rows.Next() {
		var name string
		var marker *string
		if err := rows.Scan(&name, &marker); err != nil {
			return fmt.Errorf("read active database: %w", err)
		}
		if strings.HasPrefix(name, "corpus_test_") || strings.HasPrefix(name, "corpus_tpl_") || testDatabaseName(name) || testpg.MarkerRunKey(marker) != "" {
			active = append(active, name)
		}
	}
	if rows.Err() != nil {
		return fmt.Errorf("read active databases: %w", rows.Err())
	}
	if len(active) != 0 {
		return fmt.Errorf("refusing to prune: test database sessions are connected: %s", strings.Join(active, ", "))
	}
	return nil
}

func pruneOne(ctx context.Context, admin *pgx.Conn, out io.Writer, name, current string) error {
	if reason := pruneSkipReason(name, current, 0); reason != "" {
		_, err := fmt.Fprintf(out, "skipped %s: %s\n", name, reason)
		return err
	}
	if _, err := admin.Exec(ctx, `SELECT pg_catalog.pg_advisory_lock(723548171, pg_catalog.hashtext($1))`, name); err != nil {
		return fmt.Errorf("lock template: %w", err)
	}
	defer func() {
		_, _ = admin.Exec(context.WithoutCancel(ctx), `SELECT pg_catalog.pg_advisory_unlock(723548171, pg_catalog.hashtext($1))`, name)
	}()
	var oid uint32
	var marker *string
	err := admin.QueryRow(ctx, `SELECT oid, pg_catalog.shobj_description(oid, 'pg_database') FROM pg_catalog.pg_database WHERE datname=$1`, name).Scan(&oid, &marker)
	if err == pgx.ErrNoRows {
		_, err = fmt.Fprintf(out, "skipped %s: no longer exists\n", name)
		return err
	}
	if err != nil {
		return fmt.Errorf("inspect template: %w", err)
	}
	var actualOID uint32
	var actualMarker *string
	var template bool
	var sessions int
	err = admin.QueryRow(ctx, `SELECT oid, pg_catalog.shobj_description(oid, 'pg_database'), datistemplate, (SELECT pg_catalog.count(*) FROM pg_catalog.pg_stat_activity WHERE datname=$1) FROM pg_catalog.pg_database WHERE datname=$1`, name).Scan(&actualOID, &actualMarker, &template, &sessions)
	if err == pgx.ErrNoRows {
		_, err = fmt.Fprintf(out, "skipped %s: no longer exists\n", name)
		return err
	}
	if err != nil {
		return fmt.Errorf("reinspect template: %w", err)
	}
	if oid != actualOID || !sameNullableText(marker, actualMarker) {
		_, err = fmt.Fprintf(out, "skipped %s: database identity or marker changed\n", name)
		return err
	}
	if reason := pruneSkipReason(name, current, sessions); reason != "" {
		_, err = fmt.Fprintf(out, "skipped %s: %s\n", name, reason)
		return err
	}
	if dropErr := dropTemplatePreservingFlag(ctx, admin, name, template); dropErr != nil {
		return dropErr
	}
	_, err = fmt.Fprintf(out, "dropped %s\n", name)
	return err
}

func sameNullableText(first, second *string) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
}

type templateDropExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func dropTemplatePreservingFlag(ctx context.Context, conn templateDropExecutor, name string, template bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if template {
		if err := alterTemplateFlag(ctx, conn, name, false); err != nil {
			return fmt.Errorf("unmark template %s: %w", name, err)
		}
	}
	if err := dropTemplate(ctx, conn, name); err != nil {
		if !template {
			return fmt.Errorf("drop incomplete template %s: %w", name, err)
		}
		if restoreErr := alterTemplateFlag(ctx, conn, name, true); restoreErr != nil {
			return fmt.Errorf("drop template %s: %w; restore template marker: %v", name, err, restoreErr)
		}
		return fmt.Errorf("drop template %s: %w", name, err)
	}
	return nil
}

func dropTemplate(ctx context.Context, conn templateDropExecutor, name string) error {
	statement, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	_, err := conn.Exec(statement, "DROP DATABASE "+pgx.Identifier{name}.Sanitize())
	if err != nil && (errors.Is(statement.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)) {
		return fmt.Errorf("database %q may be invalid; drop it again: %w", name, err)
	}
	return err
}

func alterTemplateFlag(ctx context.Context, conn templateDropExecutor, name string, template bool) error {
	statement, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	_, err := conn.Exec(statement, fmt.Sprintf("ALTER DATABASE %s IS_TEMPLATE %t", pgx.Identifier{name}.Sanitize(), template))
	return err
}
