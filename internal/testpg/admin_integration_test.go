//go:build integration

package testpg

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestOpenPruneAdministrator_RefusesRunningTests(t *testing.T) {
	ctx := context.Background()
	admin, _, err := OpenAdministrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	prune, err := OpenPruneAdministrator(ctx)
	if prune != nil {
		defer func() { _ = prune.Close(ctx) }()
	}
	if err == nil || !strings.Contains(err.Error(), "tests are running") {
		t.Fatalf("open prune administrator error = %v", err)
	}
}

func TestOpenPruneAdministrator_RefusesTestsOnAnotherDatabase(t *testing.T) {
	ctx := context.Background()
	raw := os.Getenv("CORPUS_TEST_ADMIN_URL")
	if raw == "" {
		raw = "postgres://trachuan@127.0.0.1:25433/postgres"
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/postgres"
	t.Setenv("CORPUS_TEST_ADMIN_URL", u.String())
	admin, _, err := OpenAdministrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	u.Path = "/template1"
	t.Setenv("CORPUS_TEST_ADMIN_URL", u.String())
	prune, err := OpenPruneAdministrator(ctx)
	if prune != nil {
		defer func() { _ = prune.Close(ctx) }()
	}
	if err == nil || !strings.Contains(err.Error(), "tests are running") {
		t.Fatalf("open prune administrator error = %v", err)
	}
}

func TestOpenAdministrator_FixesSearchPath(t *testing.T) {
	ctx := context.Background()
	raw := os.Getenv("CORPUS_TEST_ADMIN_URL")
	if raw == "" {
		raw = "postgres://trachuan@127.0.0.1:25433/postgres"
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", "public,pg_catalog")
	q.Set("options", "-csearch_path=public,pg_catalog")
	u.RawQuery = q.Encode()
	t.Setenv("CORPUS_TEST_ADMIN_URL", u.String())
	admin, _, err := OpenAdministrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	var path string
	if err := admin.QueryRow(ctx, `SHOW search_path`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if path != "pg_catalog" {
		t.Fatalf("search_path = %q, want pg_catalog", path)
	}
}
