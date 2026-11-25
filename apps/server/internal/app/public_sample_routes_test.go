package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/mcpapi"
	"danny.vn/trachuan/apps/server/internal/publicsample"
	"danny.vn/trachuan/apps/server/internal/weblogin"
	"danny.vn/trachuan/apps/server/internal/websession"
)

func TestPublicSample_MountedFlights(t *testing.T) {
	now := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	flights := publicsample.NewFlights(context.Background(), func(context.Context) (publicsample.Activation, error) {
		return publicsample.Activation{Database: "corpus_vn_20990101", ActivatedAt: now}, nil
	}, func(publicsample.Activation) bool { return true }, func(context.Context, publicsample.Activation, time.Time) ([]byte, error) {
		return []byte(`{"sample":true}`), nil
	}, func() time.Time { return now })
	defer flights.Close()
	sessions := &websession.Manager{PublicURL: "http://localhost"}
	mux := newWebMux(webDeps{publicSample: true, sampleFlights: flights, store: routeAccountStore{}, sessions: sessions, login: &weblogin.Handler{}, tools: mcpapi.Config{Limiter: &routeLimiter{}}, loginLimit: newLoginLimiter(time.Now, nil), publicURL: sessions.PublicURL})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/sample", nil))
	if w.Code != http.StatusOK || w.Body.String() != `{"sample":true}` || w.Header().Get("X-Sample-Status-Date") != "2099-01-01" {
		t.Fatalf("status %d, body %q, headers %v", w.Code, w.Body.String(), w.Header())
	}
}
