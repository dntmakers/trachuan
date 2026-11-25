//go:build integration

package app

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/publicsample"
	"danny.vn/trachuan/apps/server/internal/publicsummary"
	"danny.vn/trachuan/apps/server/internal/release"
	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPublicSample_PostgresNotReady(t *testing.T) {
	ctx := context.Background()
	admin, base, err := testpg.OpenAdministrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name, key, err := testpg.CreateCorpus(ctx, admin, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer testpg.DropCorpusDatabase(t, admin, name, key)
	auth, err := pgxpool.New(ctx, testpg.NewDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer auth.Close()
	_, err = auth.Exec(ctx, `CREATE TABLE corpus_release(country text PRIMARY KEY, active_db text, activated_at timestamptz, summary_database text, summary_projection integer, headings_approved boolean)`)
	if err != nil {
		t.Fatal(err)
	}
	base.Path = "/{database}"
	template := strings.Replace(base.String(), "%7Bdatabase%7D", "{database}", 1)
	resolver := &release.Resolver{}
	flights, closeSample := newPublicSample(ctx, auth, template, resolver, time.Now, slog.Default())
	defer closeSample()
	h := publicsample.NewHandler(true, publicsummary.NewAdmission()).WithFlights(flights)
	call := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/sample", nil))
		return w
	}
	w := call()
	if w.Code != 503 || !strings.Contains(w.Body.String(), "unavailable") {
		t.Fatalf("missing activation: %d %s", w.Code, w.Body.String())
	}
	_, err = auth.Exec(ctx, `INSERT INTO corpus_release(country, active_db, activated_at) VALUES ('vn', $1, now())`, name)
	if err != nil {
		t.Fatal(err)
	}
	w = call()
	if w.Code != 503 || !strings.Contains(w.Body.String(), "busy") {
		t.Fatalf("engine not ready: %d %s", w.Code, w.Body.String())
	}
}
