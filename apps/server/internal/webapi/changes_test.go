package webapi

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/limits"
	"danny.vn/trachuan/apps/server/internal/mcpapi"
	"danny.vn/trachuan/apps/server/internal/store"
	"danny.vn/trachuan/apps/server/internal/websession"
)

type changesSessionStore struct {
	sessionStore
	mode string
}

func (s changesSessionStore) GetSession(ctx context.Context, hash string) (store.GetSessionRow, error) {
	row, err := s.sessionStore.GetSession(ctx, hash)
	if s.mode == "expired" {
		row.ExpiresAt = s.now
	}
	if s.mode == "revoked" {
		row.RevokedAt = &s.now
	}
	return row, err
}

func TestChanges_RouteRequiresSession(t *testing.T) {
	h := newHarness(t)
	w := webCall(h, "POST", "/api/v1/changes", `{"country":"vn"}`)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	want := mcpCall(t, h, "changes", `{"country":"vn"}`)
	if !bytes.Equal(w.Body.Bytes(), want) {
		t.Fatalf("web %s MCP %s", w.Body.String(), want)
	}
	for _, mode := range []string{"missing", "expired", "revoked", "missing-csrf", "wrong-csrf", "bearer"} {
		t.Run(mode, func(t *testing.T) {
			before := h.engine.calls.Load()
			r := webRequest("POST", "/api/v1/changes", `{"country":"vn"}`)
			switch mode {
			case "missing":
				r.Header.Del("Cookie")
			case "missing-csrf":
				r.Header.Del("X-CSRF-Token")
			case "wrong-csrf":
				r.Header.Set("X-CSRF-Token", "wrong")
			case "bearer":
				r.Header.Del("Cookie")
				r.Header.Set("Authorization", "Bearer tra_"+strings.Repeat("A", 43))
			}
			out := httptest.NewRecorder()
			handler := h.web
			if mode == "expired" || mode == "revoked" {
				now := time.Date(2099, 11, 10, 0, 0, 0, 0, time.UTC)
				clock := func() time.Time { return now }
				manager := &websession.Manager{Store: changesSessionStore{sessionStore{now: now}, mode}, Clock: clock, PublicURL: "https://example.test"}
				handler = New(Config{Tools: mcpapi.Config{Resolver: h.resolver, Limiter: limits.New(clock), Clock: clock}, Sessions: manager})
			}
			handler.ServeHTTP(out, r)
			if (mode == "expired" || mode == "revoked") && out.Code != 401 {
				t.Fatalf("status %d body %s", out.Code, out.Body.String())
			}
			if out.Code < 400 || h.engine.calls.Load() != before || out.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status %d body %s", out.Code, out.Body.String())
			}
		})
	}
}
