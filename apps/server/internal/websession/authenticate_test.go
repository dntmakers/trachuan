package websession

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthenticateReadOnly_DoesNotMutateCache(t *testing.T) {
	m, _, raw := harness()
	if _, _, err := m.AuthenticateReadOnly(request(raw)); err != nil {
		t.Fatal(err)
	}
	if len(m.entries) != 0 {
		t.Fatal("read-only authentication inserted a cache entry")
	}
	hash := hashToken(raw)
	old := cacheEntry{until: time.Unix(1, 0)}
	m.entries = map[string]cacheEntry{hash: old}
	if _, _, err := m.AuthenticateReadOnly(request(raw)); err != nil {
		t.Fatal(err)
	}
	if got, ok := m.entries[hash]; !ok || got != old {
		t.Fatal("read-only authentication changed an expired entry")
	}
	m.entries = make(map[string]cacheEntry, maxEntries)
	for i := range maxEntries {
		m.entries[fmt.Sprintf("entry-%d", i)] = old
	}
	if _, _, err := m.AuthenticateReadOnly(request(raw)); err != nil {
		t.Fatal(err)
	}
	if len(m.entries) != maxEntries {
		t.Fatal("read-only authentication evicted an entry from a full cache")
	}
	if _, ok := m.entries[hash]; ok {
		t.Fatal("read-only authentication inserted into a full cache")
	}
}

func TestAuthenticate_RenewAndFailures(t *testing.T) {
	m, s, raw := harness()
	if _, _, err := m.Authenticate(request("")); err == nil {
		t.Fatal("missing session authenticated")
	}
	advance(s, 2*time.Hour)
	_, _, err := m.AuthenticateReadOnly(request(raw))
	if err != nil || s.touches != 0 {
		t.Fatal("read-only authentication touched the session")
	}
	p, renew, err := m.Authenticate(request(raw))
	if err != nil || !renew || p.AccountID != 42 {
		t.Fatalf("principal=%+v renew=%v error=%v", p, renew, err)
	}
	w := httptest.NewRecorder()
	m.Renew(w, request(raw), p)
	if len(w.Result().Cookies()) != 1 {
		t.Fatal("renewal cookie missing")
	}
	ctx := WithPrincipal(t.Context(), p)
	if got, ok := FromContext(ctx); !ok || got.AccountID != p.AccountID {
		t.Fatal("principal missing from context")
	}
	m.EvictAccount(42)
	s.fail = "get"
	if _, _, err := m.Authenticate(request(raw)); err == nil {
		t.Fatal("store failure authenticated")
	}
}
