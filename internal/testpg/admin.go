package testpg

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

func openAdministrator(ctx context.Context) (*pgx.Conn, *url.URL, error) {
	raw := os.Getenv("CORPUS_TEST_ADMIN_URL")
	if raw == "" {
		raw = "postgres://trachuan@127.0.0.1:25433/postgres"
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return nil, nil, errors.New("invalid test database URL")
	}
	for _, key := range []string{"host", "hostaddr", "dbname", "user", "password"} {
		if parsed.Query().Has(key) {
			return nil, nil, errors.New("test database URL must put connection identity in the URL authority and path")
		}
	}
	cfg, err := pgx.ParseConfig(raw)
	if err != nil {
		return nil, nil, errors.New("invalid test database configuration")
	}
	loopback := func(host string) bool {
		ip := net.ParseIP(host)
		return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
	}
	if !loopback(cfg.Host) {
		return nil, nil, errors.New("test database administrator must use a loopback host")
	}
	for _, fallback := range cfg.Fallbacks {
		if !loopback(fallback.Host) {
			return nil, nil, errors.New("test database administrator fallback must use a loopback host")
		}
	}
	cfg.Database = "postgres"
	cfg.RuntimeParams["search_path"] = "pg_catalog"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, nil, errors.New("connect to test database administrator")
	}
	return conn, parsed, nil
}

const testRunLock int64 = 7218291464872601

func OpenAdministrator(ctx context.Context) (*pgx.Conn, *url.URL, error) {
	conn, parsed, err := openAdministrator(ctx)
	if err != nil {
		return nil, nil, err
	}
	if _, err := conn.Exec(ctx, `SELECT pg_catalog.pg_advisory_lock_shared($1)`, testRunLock); err != nil {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, nil, fmt.Errorf("lock test administrator: %w", err)
	}
	return conn, parsed, nil
}

func OpenPruneAdministrator(ctx context.Context) (*pgx.Conn, error) {
	conn, _, err := openAdministrator(ctx)
	if err != nil {
		return nil, err
	}
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_catalog.pg_try_advisory_lock($1)`, testRunLock).Scan(&locked); err != nil {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("lock test pruning: %w", err)
	}
	if !locked {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, errors.New("refusing to prune: tests are running")
	}
	return conn, nil
}
