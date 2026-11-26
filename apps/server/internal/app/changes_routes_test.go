package app

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/server/internal/contracttest"
)

func TestRoutes_OpenAPIMounted(t *testing.T) {
	contract, err := contracttest.Load()
	if err != nil {
		t.Fatal(err)
	}
	handler, _, _ := routeFixture(t)
	mux := handler.(*webMux)
	params := regexp.MustCompile(`\{[^}]+\}`)
	for path, operations := range contract.Paths {
		if !strings.HasPrefix(path, "/api/v1/") {
			continue
		}
		actual := params.ReplaceAllString(path, "1")
		for method := range operations {
			if frameworkPath(actual) {
				frameworks, _ := frameworkPathRoutes(true)
				request := httptest.NewRequest(strings.ToUpper(method), actual, nil)
				request.AddCookie(&http.Cookie{Name: "__Host-session", Value: strings.Repeat("A", 43)})
				request.Header.Set("Origin", "http://localhost")
				request.Header.Set("X-CSRF-Token", strings.Repeat("A", 43))
				response := httptest.NewRecorder()
				frameworks.ServeHTTP(response, request)
				if response.Code != http.StatusTooManyRequests {
					t.Errorf("unmounted %s %s: status %d", method, path, response.Code)
				}
				continue
			}
			request := httptest.NewRequest(strings.ToUpper(method), actual, nil)
			_, pattern := mux.Handler(request)
			if pattern == "" {
				t.Errorf("unmounted %s %s", method, path)
			}
		}
	}
}
