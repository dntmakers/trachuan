package frameworkaccess

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/accountemail"
	"danny.vn/trachuan/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func TestDecision_FailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, code, class string
		all               bool
		codes             []string
		want              Decision
	}{
		{name: "free", code: "fw-alpha", class: "free", want: Full},
		{name: "licensed absent", code: "fw-alpha", class: "licensed", want: Reduced},
		{name: "licensed present", code: "fw-alpha", class: "licensed", codes: []string{"fw-alpha"}, want: Full},
		{name: "different framework", code: "fw-beta", class: "licensed", codes: []string{"fw-alpha"}, want: Reduced},
		{name: "all", code: "fw-beta", class: "licensed", all: true, want: Full},
		{name: "unknown free", code: "fw-unknown", class: "free", all: true, want: Reduced},
		{name: "unknown licensed", code: "fw-unknown", class: "licensed", all: true, want: Reduced},
		{name: "missing class", code: "fw-alpha", all: true, want: Reduced},
		{name: "unknown class", code: "fw-alpha", class: "public", all: true, want: Reduced},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := Set{known: map[string]struct{}{"fw-alpha": {}, "fw-beta": {}}, all: tc.all, codes: map[string]struct{}{}}
			for _, code := range tc.codes {
				set.codes[code] = struct{}{}
			}
			if got := Decide(tc.code, tc.class, set); got != tc.want {
				t.Fatalf("decision=%v want=%v", got, tc.want)
			}
		})
	}
	if got := Decide("fw-alpha", "free", Set{}); got != Reduced {
		t.Fatalf("empty set decision=%v", got)
	}
}

type accessStore struct {
	account                    store.Account
	accountErr, entitlementErr error
	email                      string
	rows                       []store.FrameworkEntitlement
}

func (s *accessStore) FrameworkAccessSnapshot(_ context.Context, id int64) (store.FrameworkAccessSnapshot, error) {
	if id != 42 {
		return store.FrameworkAccessSnapshot{}, pgx.ErrNoRows
	}
	if s.accountErr != nil {
		return store.FrameworkAccessSnapshot{}, s.accountErr
	}
	if s.entitlementErr != nil {
		return store.FrameworkAccessSnapshot{}, s.entitlementErr
	}
	s.email, _ = accountemail.Parse(s.account.Email)
	return store.FrameworkAccessSnapshot{Account: s.account, Entitlements: append([]store.FrameworkEntitlement(nil), s.rows...)}, nil
}

func (s *accessStore) GetAccount(_ context.Context, id int64) (store.Account, error) {
	if id != 42 {
		return store.Account{}, pgx.ErrNoRows
	}
	return s.account, s.accountErr
}
func (s *accessStore) ListLiveFrameworkEntitlementsByEmail(_ context.Context, email string) ([]store.FrameworkEntitlement, error) {
	s.email = email
	return s.rows, s.entitlementErr
}

type changingAccessStore struct {
	*accessStore
}

func (s *changingAccessStore) FrameworkAccessSnapshot(ctx context.Context, id int64) (store.FrameworkAccessSnapshot, error) {
	snapshot, err := s.accessStore.FrameworkAccessSnapshot(ctx, id)
	blocked := time.Now()
	s.account.BlockedAt = &blocked
	s.rows = []store.FrameworkEntitlement{{Email: s.account.Email, Scope: "fw-alpha"}}
	return snapshot, err
}

func TestEntitled_BlockAndGrantCannotCombineSnapshots(t *testing.T) {
	s := &changingAccessStore{&accessStore{account: store.Account{ID: 42, Email: "Owner@example.test"}}}
	service := Service{Store: s}
	if service.Entitled(t.Context(), 42) {
		t.Fatal("pre-block snapshot admitted a later grant")
	}
	if service.Entitled(t.Context(), 42) {
		t.Fatal("post-block snapshot admitted the grant")
	}
}

func TestEntitled_RechecksLiveAccess(t *testing.T) {
	s := &accessStore{account: store.Account{ID: 42, Email: "Owner@example.test"}}
	service := Service{Store: s, Frameworks: []string{"fw-alpha"}}
	if service.Entitled(t.Context(), 42) {
		t.Fatal("empty access set is entitled")
	}
	s.rows = []store.FrameworkEntitlement{{Email: "Owner@example.test", Scope: "fw-alpha"}}
	if !service.Entitled(t.Context(), 42) {
		t.Fatal("live entitlement was not recognized")
	}
	if service.Entitled(t.Context(), 0) {
		t.Fatal("unauthenticated caller is entitled")
	}
	s.entitlementErr = errors.New("store unavailable")
	if service.Entitled(t.Context(), 42) {
		t.Fatal("store failure is entitled")
	}
	s.entitlementErr = nil
	if !service.Entitled(t.Context(), 42) {
		t.Fatal("store failure was cached")
	}
	revoked := time.Now()
	s.rows[0].RevokedAt = &revoked
	if service.Entitled(t.Context(), 42) {
		t.Fatal("revocation was not observed")
	}
	s.rows = []store.FrameworkEntitlement{{Email: "Owner@example.test", Scope: "fw-other"}}
	if !service.Entitled(t.Context(), 42) {
		t.Fatal("live entitlement outside the active release was not recognized")
	}
	service.Frameworks = nil
	if !service.Entitled(t.Context(), 42) {
		t.Fatal("live entitlement requires a release")
	}
	s.rows[0].Scope = "*"
	if !service.Entitled(t.Context(), 42) {
		t.Fatal("wildcard entitlement was not recognized")
	}
	s.rows[0].Email = "other@example.test"
	if service.Entitled(t.Context(), 42) {
		t.Fatal("another email's entitlement was recognized")
	}
	s.rows[0].Email = "Owner@example.test"
	s.account.Email = "invalid"
	if service.Entitled(t.Context(), 42) {
		t.Fatal("invalid account email was entitled")
	}
	s.account.Email = "Owner@example.test"
	s.accountErr = errors.New("store unavailable")
	if service.Entitled(t.Context(), 42) {
		t.Fatal("account store failure is entitled")
	}
}

func TestAccessSet_StoreFailureIsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		accountErr, entitlementErr error
	}{
		{name: "account", accountErr: errors.New("synthetic private store error")},
		{name: "entitlement", entitlementErr: errors.New("synthetic private store error")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &accessStore{account: store.Account{ID: 42, Email: "Owner@example.test"}, accountErr: tc.accountErr, entitlementErr: tc.entitlementErr}
			service := Service{Store: s, Frameworks: []string{"fw-alpha"}}
			set, err := service.AccessSet(t.Context(), 42)
			if !errors.Is(err, ErrUnavailable) || err.Error() != "framework access unavailable" {
				t.Fatalf("error=%v", err)
			}
			for _, class := range []string{"free", "licensed", ""} {
				if Decide("fw-alpha", class, set) != Reduced {
					t.Fatal("store failure allowed access")
				}
			}
		})
	}
}
func TestAccessSet_CanonicalEmailAndKnownScopes(t *testing.T) {
	s := &accessStore{account: store.Account{ID: 42, Email: " \tOwner@EXAMPLE.test\n"}, rows: []store.FrameworkEntitlement{
		{Email: "Owner@example.test", Scope: "fw-alpha"},
		{Email: "Owner@example.test", Scope: "fw-unknown"},
		{Email: "other@example.test", Scope: "*"},
	}}
	service := Service{Store: s, Frameworks: []string{"fw-alpha", "fw-beta"}}
	set, err := service.AccessSet(t.Context(), 42)
	if err != nil || s.email != "Owner@example.test" || !reflect.DeepEqual(set.Scopes(), []string{"fw-alpha"}) {
		t.Fatalf("scopes=%v email=%q error=%v", set.Scopes(), s.email, err)
	}
	if Decide("fw-alpha", "licensed", set) != Full || Decide("fw-beta", "licensed", set) != Reduced {
		t.Fatal("incorrect scope decision")
	}
	s.rows = []store.FrameworkEntitlement{{Email: "Owner@example.test", Scope: "*"}}
	set, err = service.AccessSet(t.Context(), 42)
	if err != nil || !reflect.DeepEqual(set.Scopes(), []string{"*"}) || Decide("fw-unknown", "licensed", set) != Reduced {
		t.Fatal("wildcard admitted unknown framework")
	}
	set, err = service.AccessSet(t.Context(), 99)
	if err != nil || len(set.Scopes()) != 0 || Decide("fw-alpha", "free", set) != Reduced {
		t.Fatal("missing account gained access")
	}
}
