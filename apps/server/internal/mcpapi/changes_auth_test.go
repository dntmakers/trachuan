package mcpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/credential"
	"danny.vn/trachuan/apps/server/internal/limits"
	"danny.vn/trachuan/apps/server/internal/store"
)

type changesAuthStore struct{ row store.GetTokenRow }

func (s changesAuthStore) GetCredential(context.Context, string) (store.GetCredentialRow, error) {
	return store.GetCredentialRow{}, nil
}
func (s changesAuthStore) TouchPersonalToken(context.Context, string) error { return nil }
func (s changesAuthStore) GetToken(context.Context, string) (store.GetTokenRow, error) {
	return s.row, nil
}
func (s changesAuthStore) TouchOAuthGrant(context.Context, int64) error { return nil }

func TestChanges_UnauthorizedDoesNotReachEngine(t *testing.T) {
	now := time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		status int
	}{{"missing", 401}, {"expired", 401}, {"revoked", 401}, {"scope", 403}} {
		t.Run(tc.name, func(t *testing.T) {
			row := store.GetTokenRow{Kind: "access", AccountID: 7, GrantID: 9, Resource: "https://example.test/mcp", Scope: "mcp:read", ExpiresAt: now.Add(time.Hour), FamilyExpiresAt: now.Add(time.Hour)}
			switch tc.name {
			case "expired":
				row.ExpiresAt = now
			case "revoked":
				row.RevokedAt = &now
			case "scope":
				row.Scope = "other"
			}
			engine := &fakeEngine{}
			resolver := &fakeResolver{engine: engine}
			handler := credential.Middleware(&credential.Verifier{Store: changesAuthStore{row}, Audience: "https://example.test/mcp", Clock: func() time.Time { return now }}, New(Config{Resolver: resolver, Limiter: limits.New(time.Now)}))
			request := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"changes","arguments":{"country":"vn"}}}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			if tc.name != "missing" {
				request.Header.Set("Authorization", "Bearer tra_"+strings.Repeat("A", 43))
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status || engine.calls.Load() != 0 {
				t.Fatalf("status %d, engine calls %d", response.Code, engine.calls.Load())
			}
		})
	}
}
