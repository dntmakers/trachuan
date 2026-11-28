package app

import (
	"context"
	"net/http"
	"strings"

	"danny.vn/trachuan/apps/server/internal/apierror"
	"danny.vn/trachuan/apps/server/internal/frameworkaccess"
	"danny.vn/trachuan/apps/server/internal/mcpapi"
	"danny.vn/trachuan/apps/server/internal/websession"
)

type webMux struct {
	*http.ServeMux
	frameworks http.Handler
}

func (m *webMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if frameworkPath(r.URL.EscapedPath()) || frameworkPath(r.URL.Path) {
		m.frameworks.ServeHTTP(w, r)
		return
	}
	m.ServeMux.ServeHTTP(w, r)
}

func frameworkPath(path string) bool {
	return path == "/api/v1/frameworks" || strings.HasPrefix(path, "/api/v1/frameworks/")
}

func guardFrameworkAdmission(limiter *loginLimiter, next http.Handler) http.Handler {
	return mcpapi.WithCacheControl("no-store", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, _ := limiter.allow(limiter.clientIP(r)); !ok {
			apierror.Write(w, 404, "not_found")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func guardEntitledFrameworkRoutes(sessions *websession.Manager, check func(context.Context, int64) bool, enabled bool, next http.Handler) http.Handler {
	off := guardFrameworkRoutes(false, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sessions == nil {
			off.ServeHTTP(w, r)
			return
		}
		r = r.WithContext(frameworkaccess.RequestContext(r.Context()))
		p, _, err := sessions.AuthenticateReadOnly(r)
		if err != nil || !check(r.Context(), p.AccountID) || !enabled {
			off.ServeHTTP(w, r)
			return
		}
		if frameworkPathError(r) != "" {
			next.ServeHTTP(w, r.WithContext(websession.WithPrincipal(r.Context(), p)))
			return
		}
		verified, renew, err := sessions.Authenticate(r)
		if err != nil {
			w.Header().Set("Cache-Control", "no-store")
			websession.WriteAuthenticationError(w, r, err)
			return
		}
		if verified.AccountID != p.AccountID || verified.SessionID != p.SessionID || verified.AuthEpoch != p.AuthEpoch {
			w.Header().Set("Cache-Control", "no-store")
			apierror.Write(w, 401, "unauthenticated")
			return
		}
		p = verified
		if renew {
			sessions.Renew(w, r, p)
		}
		next.ServeHTTP(w, r.WithContext(websession.WithPrincipal(r.Context(), p)))
	})
}

func guardFrameworkRoutes(enabled bool, next http.Handler) http.Handler {
	return mcpapi.WithCacheControl("no-store", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !enabled {
			apierror.Write(w, 404, "not_found")
			return
		}
		if code := frameworkPathError(r); code != "" {
			apierror.Write(w, 400, code)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func frameworkPathError(r *http.Request) string {
	malformed := strings.Contains(strings.ToLower(r.URL.EscapedPath()), "%2f")
	for _, segment := range strings.Split(r.URL.Path, "/")[1:] {
		if segment == "" || segment == "." || segment == ".." {
			malformed = true
			break
		}
	}
	if !malformed {
		return ""
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/frameworks/controls/") || strings.HasPrefix(r.URL.Path, "/api/v1/frameworks/versions/") {
		return "invalid_handle"
	}
	return "invalid_request"
}
