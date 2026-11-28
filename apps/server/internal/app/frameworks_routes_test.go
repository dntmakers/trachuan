package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/mcpapi"
	"danny.vn/trachuan/apps/server/internal/store"
	"danny.vn/trachuan/apps/server/internal/weblogin"
	"danny.vn/trachuan/apps/server/internal/websession"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func frameworkPathRoutes(enabled bool) (http.Handler, *routeLimiter) {
	limiter := &routeLimiter{}
	sessions := &websession.Manager{Store: routeSessionStore{}, PublicURL: "http://localhost"}
	return newWebMux(webDeps{store: routeAccountStore{}, sessions: sessions, login: &weblogin.Handler{}, tools: mcpapi.Config{Frameworks: enabled, Limiter: limiter}, loginLimit: newLoginLimiter(time.Now, nil), publicURL: sessions.PublicURL}), limiter
}

type frameworkSessionStore struct {
	routeSessionStore
	reads       int
	touches     int
	vanishAfter int
	stale       bool
	fail        bool
	failTouch   bool
}

func (s *frameworkSessionStore) GetSession(ctx context.Context, hash string) (store.GetSessionRow, error) {
	s.reads++
	if s.vanishAfter > 0 && s.reads >= s.vanishAfter {
		return store.GetSessionRow{}, pgx.ErrNoRows
	}
	if s.fail {
		return store.GetSessionRow{}, errors.New("session store failure")
	}
	row, err := s.routeSessionStore.GetSession(ctx, hash)
	if s.stale {
		row.LastSeenAt = row.LastSeenAt.Add(-2 * time.Hour)
	}
	return row, err
}

func (s *frameworkSessionStore) TouchSession(context.Context, string) (int64, error) {
	s.touches++
	if s.failTouch {
		return 0, errors.New("session touch failure")
	}
	s.stale = false
	return 1, nil
}

func TestFrameworkRoutes_DeniedSessionCanRenewLater(t *testing.T) {
	s := &frameworkSessionStore{stale: true}
	sessions := &websession.Manager{Store: s, PublicURL: "http://localhost"}
	guard := guardEntitledFrameworkRoutes(sessions, func(context.Context, int64) bool { return false }, true, http.NotFoundHandler())
	r := httptest.NewRequest("GET", "/api/v1/frameworks", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-session", Value: strings.Repeat("A", 43)})
	w := httptest.NewRecorder()
	guard.ServeHTTP(w, r)
	require.Equal(t, 404, w.Code)
	require.Zero(t, s.touches)
	w = httptest.NewRecorder()
	sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
	require.Equal(t, 204, w.Code)
	require.Equal(t, 1, s.touches)
	require.Len(t, w.Result().Cookies(), 1)
}

func TestFrameworkRoutes_MalformedSessionCanRenewLater(t *testing.T) {
	s := &frameworkSessionStore{stale: true}
	sessions := &websession.Manager{Store: s, PublicURL: "http://localhost"}
	guard := guardEntitledFrameworkRoutes(sessions, func(context.Context, int64) bool { return true }, true, guardFrameworkRoutes(true, http.NotFoundHandler()))
	r := httptest.NewRequest("GET", "/api/v1/frameworks//bad", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-session", Value: strings.Repeat("A", 43)})
	w := httptest.NewRecorder()
	guard.ServeHTTP(w, r)
	require.Equal(t, 400, w.Code)
	require.Zero(t, s.touches)
	w = httptest.NewRecorder()
	sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
	require.Equal(t, 204, w.Code)
	require.Equal(t, 1, s.touches)
	require.Len(t, w.Result().Cookies(), 1)
}

func TestFrameworkRoutes_AcceptedSessionFailureMatchesTrueMode(t *testing.T) {
	request := func(h http.Handler) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/frameworks", nil)
		r.AddCookie(&http.Cookie{Name: "__Host-session", Value: strings.Repeat("A", 43)})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, failure := range []string{"touch", "revoked"} {
		t.Run(failure, func(t *testing.T) {
			build := func(entitled bool) http.Handler {
				s := &frameworkSessionStore{stale: true}
				if failure == "touch" {
					s.failTouch = true
				} else if entitled {
					s.vanishAfter = 2
				} else {
					s.vanishAfter = 1
				}
				sessions := &websession.Manager{Store: s, PublicURL: "http://localhost"}
				base := guardFrameworkRoutes(true, sessions.Middleware(http.NotFoundHandler()))
				if entitled {
					return guardEntitledFrameworkRoutes(sessions, func(context.Context, int64) bool { return true }, true, base)
				}
				return base
			}
			got, want := request(build(true)), request(build(false))
			if got.Code != want.Code || got.Body.String() != want.Body.String() || !reflect.DeepEqual(got.Header(), want.Header()) {
				t.Fatalf("got %d %s headers=%v, want %d %s headers=%v", got.Code, got.Body.String(), got.Header(), want.Code, want.Body.String(), want.Header())
			}
		})
	}
}

func TestFrameworkRoutes_EntitledChecksSessionTwice(t *testing.T) {
	store := &frameworkSessionStore{}
	sessions := &websession.Manager{Store: store, PublicURL: "http://localhost"}
	checks := 0
	tools := mcpapi.Config{Frameworks: true, FrameworkEntitled: func(context.Context, int64) bool { checks++; return true }, Limiter: &routeLimiter{}}
	h := newWebMux(webDeps{store: routeAccountStore{}, sessions: sessions, login: &weblogin.Handler{}, tools: tools, loginLimit: newLoginLimiter(time.Now, nil), publicURL: sessions.PublicURL})
	r := httptest.NewRequest("GET", "/api/v1/frameworks", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-session", Value: strings.Repeat("A", 43)})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 429, w.Code)
	require.Equal(t, 2, store.reads)
	require.Equal(t, 1, checks)
	sessions.EvictAccount(1)
	store.fail = true
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 404, w.Code)
	require.Equal(t, 1, checks)
}

func TestFrameworkRoutes_DisabledNeverRenews(t *testing.T) {
	s := &frameworkSessionStore{stale: true}
	sessions := &websession.Manager{Store: s, PublicURL: "http://localhost"}
	calls := 0
	tools := mcpapi.Config{FrameworkEntitled: func(context.Context, int64) bool { calls++; return true }, Limiter: &routeLimiter{}}
	h := newWebMux(webDeps{store: routeAccountStore{}, sessions: sessions, login: &weblogin.Handler{}, tools: tools, loginLimit: newLoginLimiter(time.Now, nil), publicURL: sessions.PublicURL})
	r := httptest.NewRequest("GET", "/api/v1/frameworks", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-session", Value: strings.Repeat("A", 43)})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 404, w.Code)
	require.Equal(t, 1, calls)
	require.Zero(t, s.touches)
	require.Empty(t, w.Result().Cookies())
}

func TestFrameworkRoutes_BoundsUnknownSessionLookups(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	for _, enabled := range []bool{false, true} {
		s := &frameworkSessionStore{fail: true}
		sessions := &websession.Manager{Store: s, PublicURL: "http://localhost"}
		tools := mcpapi.Config{Frameworks: enabled, FrameworkEntitled: func(context.Context, int64) bool { return false }, Limiter: &routeLimiter{}}
		h := newWebMux(webDeps{store: routeAccountStore{}, sessions: sessions, login: &weblogin.Handler{}, tools: tools, loginLimit: newLoginLimiter(clock, nil), publicURL: sessions.PublicURL})
		for range 121 {
			r := httptest.NewRequest("GET", "/api/v1/frameworks", nil)
			r.AddCookie(&http.Cookie{Name: "__Host-session", Value: strings.Repeat("A", 43)})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, 404, w.Code)
		}
		if s.reads != 120 {
			t.Fatalf("enabled=%v: session lookups=%d, want 120", enabled, s.reads)
		}
	}
}

func TestFrameworkRoutes_EntitledMatchesModes(t *testing.T) {
	allowed := false
	build := func(enabled bool, check func(context.Context, int64) bool) http.Handler {
		sessions := &websession.Manager{Store: routeSessionStore{}, PublicURL: "http://localhost"}
		tools := mcpapi.Config{Frameworks: enabled, FrameworkEntitled: check, Limiter: &routeLimiter{}}
		return newWebMux(webDeps{store: routeAccountStore{}, sessions: sessions, login: &weblogin.Handler{}, tools: tools, loginLimit: newLoginLimiter(time.Now, nil), publicURL: sessions.PublicURL})
	}
	dynamic := build(true, func(context.Context, int64) bool { return allowed })
	off, on := build(false, nil), build(true, nil)
	for _, value := range []bool{false, true, false} {
		allowed = value
		expected := off
		if value {
			expected = on
		}
		for _, route := range []struct{ method, path string }{{"GET", "/api/v1/frameworks"}, {"GET", "/api/v1/frameworks/versions/fw:fw-alpha:1"}, {"GET", "/api/v1/frameworks/controls/fw:fw-alpha:1%23A"}, {"POST", "/api/v1/frameworks/search"}, {"GET", "/api/v1/frameworks/mappings"}, {"GET", "/api/v1/frameworks/gaps"}, {"GET", "/api/v1/frameworks//bad"}} {
			for _, cookie := range []bool{false, true} {
				request := func(h http.Handler) *httptest.ResponseRecorder {
					r := httptest.NewRequest(route.method, route.path, nil)
					r.Header.Set("Origin", "http://localhost")
					r.Header.Set("X-CSRF-Token", strings.Repeat("A", 43))
					if cookie {
						r.AddCookie(&http.Cookie{Name: "__Host-session", Value: strings.Repeat("A", 43)})
					}
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					return w
				}
				comparison := expected
				if !cookie {
					comparison = off
				}
				got, want := request(dynamic), request(comparison)
				if got.Code != want.Code || got.Body.String() != want.Body.String() || !reflect.DeepEqual(got.Header(), want.Header()) {
					t.Fatalf("allowed=%v cookie=%v path=%s got %d %s headers=%v want %d %s headers=%v", value, cookie, route.path, got.Code, got.Body.String(), got.Header(), want.Code, want.Body.String(), want.Header())
				}
			}
		}
	}
}

func TestFrameworkRoutes_MalformedPathsNeverRedirect(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, path := range []string{
			"/api/v1/frameworks/controls/..",
			"/api/v1/frameworks/controls/.",
			"/api/v1/frameworks/versions/..",
			"/api/v1/frameworks/versions/.",
			"/api/v1/frameworks/controls/%2e%2e",
			"/api/v1/frameworks/controls/%2E",
			"/api/v1/frameworks//controls/fw:fw-alpha:1%23A",
			"/api/v1/frameworks/controls//fw:fw-alpha:1%23A",
			"/api/v1/frameworks/controls/fw:fw-alpha:1%23A%2FB",
			"/api/v1/frameworks/controls%2ffw:fw-alpha:1%23A",
			"/api/v1/frameworks%2Fcontrols/fw:fw-alpha:1%23A",
			"/api/v1/frameworks/controls/%2F..",
			"/api/v1/frameworks/../status",
			"/api/v1/frameworks/./search",
		} {
			for _, method := range []string{"GET", "POST", "CONNECT"} {
				t.Run(fmt.Sprintf("%t/%s%s", enabled, method, path), func(t *testing.T) {
					h, limiter := frameworkPathRoutes(enabled)
					r := httptest.NewRequest(method, path, nil)
					r.AddCookie(&http.Cookie{Name: "__Host-session", Value: strings.Repeat("A", 43)})
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					want, code := 404, "not_found"
					if enabled {
						want, code = 400, "invalid_request"
						if strings.HasPrefix(r.URL.Path, "/api/v1/frameworks/controls/") || strings.HasPrefix(r.URL.Path, "/api/v1/frameworks/versions/") {
							code = "invalid_handle"
						}
					}
					require.Equal(t, want, w.Code)
					require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
					require.Empty(t, w.Header().Get("Location"))
					require.Contains(t, w.Body.String(), `"code":"`+code+`"`)
					require.Empty(t, limiter.calls)
				})
			}
		}
	}
}

func TestFrameworkRoutes_CanonicalPathsKeepSessionGuards(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, path := range []string{
			"/api/v1/frameworks",
			"/api/v1/frameworks/controls/fw:fw-alpha:1%23A%252FB",
			"/api/v1/frameworks/versions/fw:fw-alpha:1",
			"/api/v1/frameworks/mappings",
			"/api/v1/frameworks/gaps",
		} {
			t.Run(fmt.Sprintf("%t/%s", enabled, path), func(t *testing.T) {
				h, _ := frameworkPathRoutes(enabled)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				want := 404
				if enabled {
					want = 401
				}
				require.Equal(t, want, w.Code)
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			})
		}
	}
	for _, path := range []string{"/api/v1/frameworks", "/api/v1/frameworks/controls/fw:fw-alpha:1%23A%252FB"} {
		h, limiter := frameworkPathRoutes(true)
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "__Host-session", Value: strings.Repeat("A", 43)})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, 429, w.Code)
		require.Len(t, limiter.calls, 1)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
}
