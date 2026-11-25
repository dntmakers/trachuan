package publicsample

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/server/internal/publicsummary"
)

func TestHandler_RequestRules(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, body string
		enabled                    bool
		status                     int
		code                       string
	}{
		{"off", "GET", "/api/v1/public/sample", "", false, 404, "not_found"},
		{"query before flag", "GET", "/api/v1/public/sample?x=1", "", false, 400, "invalid_request"},
		{"empty query", "GET", "/api/v1/public/sample?", "", true, 400, "invalid_request"},
		{"body before flag", "GET", "/api/v1/public/sample", "x", false, 400, "invalid_request"},
		{"enabled", "GET", "/api/v1/public/sample", "", true, 503, "unavailable"},
		{"head", "HEAD", "/api/v1/public/sample", "", true, 405, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			r.Header.Set("Cookie", "session=marker")
			r.Header.Set("Authorization", "Bearer marker")
			w := httptest.NewRecorder()
			NewHandler(tc.enabled, publicsummary.NewAdmission()).ServeHTTP(w, r)
			if w.Code != tc.status || (tc.code != "" && !strings.Contains(w.Body.String(), tc.code)) {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" || w.Header().Get("Last-Modified") != "" || w.Header().Get("Set-Cookie") != "" {
				t.Fatalf("unexpected headers %v", w.Header())
			}
		})
	}
}

type emptyRead struct{}

func (emptyRead) Read([]byte) (int, error) { return 0, nil }

func TestHandler_IndeterminateBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/public/sample", nil)
	r.Body = io.NopCloser(emptyRead{})
	r.ContentLength = -1
	w := httptest.NewRecorder()
	NewHandler(false, publicsummary.NewAdmission()).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}

func TestHandler_UnknownBodyLength(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/public/sample", strings.NewReader("x"))
	r.ContentLength = -1
	w := httptest.NewRecorder()
	NewHandler(false, publicsummary.NewAdmission()).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}

func TestHandler_Admission(t *testing.T) {
	gate := publicsummary.NewAdmission()
	for range 2 {
		if !gate.TrySample() {
			t.Fatal("admission failed")
		}
	}
	w := httptest.NewRecorder()
	NewHandler(false, gate).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/sample", nil))
	if w.Code != 404 || !strings.Contains(w.Body.String(), "not_found") {
		t.Fatalf("disabled while full: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	NewHandler(true, gate).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/sample", nil))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "busy") || w.Header().Get("Retry-After") != "5" {
		t.Fatalf("status %d headers %v body %s", w.Code, w.Header(), w.Body.String())
	}
}
