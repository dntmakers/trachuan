//go:build integration

package testdb

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPruneOne_IsolatedTemplates(t *testing.T) {
	ctx := context.Background()
	admin, base, err := testpg.OpenAdministrator(ctx)
	if err != nil {
		t.Fatal("connect to test database administrator")
	}
	t.Cleanup(func() { _ = admin.Close(ctx) })
	for _, tc := range []struct {
		name                       string
		marked, connected, current bool
		want                       string
		exists                     bool
	}{
		{"marked", true, false, false, "dropped", false},
		{"unmarked", false, false, false, "dropped", false},
		{"connected", false, true, false, "connected sessions", true},
		{"current", false, false, true, "current migration", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var random [6]byte
			if _, err := rand.Read(random[:]); err != nil {
				t.Fatal(err)
			}
			name := "corpus_tpl_" + hex.EncodeToString(random[:])
			identifier := pgx.Identifier{name}.Sanitize()
			current := ""
			if tc.current {
				current = name
			} else {
				if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := admin.Exec(ctx, "ALTER DATABASE "+identifier+" IS_TEMPLATE false"); err != nil {
						var exists bool
						if scanErr := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&exists); scanErr != nil || exists {
							t.Errorf("unmark cleanup template: %v", err)
						}
					}
					if err := dropIfExists(ctx, admin, name); err != nil {
						t.Errorf("drop cleanup template: %v", err)
					}
				})
			}
			if tc.marked {
				if _, err := admin.Exec(ctx, "ALTER DATABASE "+identifier+" IS_TEMPLATE true"); err != nil {
					t.Fatal(err)
				}
			}
			if tc.connected {
				parsed := *base
				parsed.Path = "/" + name
				conn, err := pgx.Connect(ctx, parsed.String())
				if err != nil {
					t.Fatal("connect to test template")
				}
				t.Cleanup(func() { _ = conn.Close(ctx) })
				if err := refuseActiveTests(ctx, admin); err == nil {
					t.Error("active test session was not refused")
				}
			}
			var out bytes.Buffer
			if err := pruneOne(ctx, admin, &out, name, current); err != nil {
				t.Fatal(err)
			}
			if tc.connected {
				parsed := *base
				parsed.Path = "/" + name
				second, err := pgx.Connect(ctx, parsed.String())
				if err != nil {
					t.Fatalf("reconnect after skip: %v", err)
				}
				if err := second.Close(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("output = %q, want %q", out.String(), tc.want)
			}
			var exists bool
			if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if exists != tc.exists {
				t.Errorf("exists = %t, want %t", exists, tc.exists)
			}
		})
	}
}

func TestRefuseActiveTests_MarkedDatabase(t *testing.T) {
	ctx := context.Background()
	admin, base, err := testpg.OpenAdministrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	name, key, err := testpg.CreateCorpus(ctx, admin, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { testpg.DropCorpusDatabase(t, admin, name, key) }()
	temporary := "corpus_tmp_vn_" + strings.TrimPrefix(name, "corpus_vn_")
	if _, err = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" RENAME TO "+pgx.Identifier{temporary}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	name = temporary
	base.Path = "/" + name
	conn, err := pgx.Connect(ctx, base.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if err := refuseActiveTests(ctx, admin); err == nil || !strings.Contains(err.Error(), name) {
		t.Fatalf("refuseActiveTests error = %v, want database %q", err, name)
	}
}

func TestPruneBuilderRoles_OnlyProvidedRole(t *testing.T) {
	ctx := context.Background()
	admin, _, err := testpg.OpenAdministrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	name, key, err := testpg.CreateCorpus(ctx, admin, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	role := ""
	roleCreated := false
	defer func() {
		testpg.DropCorpusDatabase(t, admin, name, key)
		if roleCreated {
			if _, cleanupErr := admin.Exec(ctx, "DROP ROLE IF EXISTS "+pgx.Identifier{role}.Sanitize()); cleanupErr != nil {
				t.Errorf("drop test role: %v", cleanupErr)
			}
		}
	}()
	var random [12]byte
	if _, err = rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	role = "test_builder_" + hex.EncodeToString(random[:])
	if _, err = admin.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	roleCreated = true
	if _, err = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" OWNER TO "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = pruneBuilderRoles(ctx, admin, &out, []string{role}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "owns a database or has connected sessions") {
		t.Fatalf("output = %q", out.String())
	}
	if !testpg.DropCorpusDatabase(t, admin, name, key) {
		t.Fatal("drop test corpus")
	}
	out.Reset()
	if err = pruneBuilderRoles(ctx, admin, &out, []string{role}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "dropped role "+role) {
		t.Fatalf("output = %q", out.String())
	}
}

func TestPrune_RefusesWhenTestsHoldLock(t *testing.T) {
	ctx := context.Background()
	admin, _, err := testpg.OpenAdministrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	prune, err := testpg.OpenPruneAdministrator(ctx)
	if prune != nil {
		defer func() { _ = prune.Close(ctx) }()
	}
	if err == nil || !strings.Contains(err.Error(), "tests are running") {
		t.Fatalf("prune lock error = %v, want running test refusal", err)
	}
}

type cancelTemplateDropExecutor struct {
	conn     *pgx.Conn
	observer *pgx.Conn
	cancel   context.CancelFunc
	observed bool
}

func (e *cancelTemplateDropExecutor) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(sql, "DROP DATABASE ") {
		return e.conn.Exec(ctx, sql, args...)
	}
	watchCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	observed := make(chan bool, 1)
	pid := e.conn.PgConn().PID()
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			var active bool
			err := e.observer.QueryRow(watchCtx, `SELECT EXISTS (SELECT FROM pg_catalog.pg_stat_activity WHERE pid=$1 AND state='active' AND pg_catalog.left(query, 14)='DROP DATABASE ')`, pid).Scan(&active)
			if err != nil {
				observed <- false
				return
			}
			if active {
				e.cancel()
				observed <- true
				return
			}
			select {
			case <-watchCtx.Done():
				observed <- false
				return
			case <-ticker.C:
			}
		}
	}()
	tag, err := e.conn.Exec(ctx, sql, args...)
	stop()
	e.observed = <-observed
	return tag, err
}

func TestDropTemplate_CancellationRestoresRealTemplate(t *testing.T) {
	ctx := context.Background()
	admin, base, err := testpg.OpenAdministrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(ctx) })
	name, key, err := testpg.CreateCorpus(ctx, admin, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	identifier := pgx.Identifier{name}.Sanitize()
	t.Cleanup(func() {
		cleanupAdmin, _, cleanupErr := testpg.OpenAdministrator(ctx)
		if cleanupErr != nil {
			t.Errorf("open cleanup administrator: %v", cleanupErr)
			return
		}
		defer func() { _ = cleanupAdmin.Close(ctx) }()
		if _, cleanupErr := cleanupAdmin.Exec(ctx, "ALTER DATABASE "+identifier+" IS_TEMPLATE false"); cleanupErr != nil {
			t.Errorf("unmark test template: %v", cleanupErr)
			return
		}
		testpg.DropCorpusDatabase(t, cleanupAdmin, name, key)
	})
	if _, execErr := admin.Exec(ctx, "ALTER DATABASE "+identifier+" IS_TEMPLATE true"); execErr != nil {
		t.Fatal(execErr)
	}
	base.Path = "/" + name
	active, err := pgx.Connect(ctx, base.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = active.Close(ctx) })
	observer, _, err := testpg.OpenAdministrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = observer.Close(ctx) })
	caller, cancel := context.WithCancel(ctx)
	defer cancel()
	executor := &cancelTemplateDropExecutor{conn: admin, observer: observer, cancel: cancel}
	err = dropTemplatePreservingFlag(caller, executor, name, true)
	if !executor.observed {
		t.Fatal("drop was not observed in progress")
	}
	if err == nil || caller.Err() != context.Canceled {
		t.Fatalf("drop error=%v caller error=%v", err, caller.Err())
	}
	var template bool
	if err := admin.QueryRow(ctx, `SELECT datistemplate FROM pg_catalog.pg_database WHERE datname=$1`, name).Scan(&template); err != nil || !template {
		t.Fatalf("template=%t error=%v", template, err)
	}
}
