package app

import (
	"context"
	"errors"
	"testing"

	"danny.vn/trachuan/apps/server/internal/frameworkaccess"
	"danny.vn/trachuan/apps/server/internal/store"
)

type frameworkAccessStore struct {
	accountErr, entitlementErr error
	entitlementCalls           int
}

func (s *frameworkAccessStore) FrameworkAccessSnapshot(ctx context.Context, id int64) (store.FrameworkAccessSnapshot, error) {
	account, err := s.GetAccount(ctx, id)
	if err != nil {
		return store.FrameworkAccessSnapshot{}, err
	}
	rows, err := s.ListLiveFrameworkEntitlementsByEmail(ctx, account.Email)
	return store.FrameworkAccessSnapshot{Account: account, Entitlements: rows}, err
}

func (s *frameworkAccessStore) GetAccount(context.Context, int64) (store.Account, error) {
	return store.Account{ID: 7, Email: "invented@example.test"}, s.accountErr
}

func (s *frameworkAccessStore) ListLiveFrameworkEntitlementsByEmail(context.Context, string) ([]store.FrameworkEntitlement, error) {
	s.entitlementCalls++
	return []store.FrameworkEntitlement{{Email: "invented@example.test", Scope: "fw-alpha"}}, s.entitlementErr
}

func TestFrameworkAccess_ErrorNotCachedInRequest(t *testing.T) {
	s := &frameworkAccessStore{entitlementErr: errors.New("invented failure")}
	ctx := frameworkaccess.RequestContext(t.Context())
	service := frameworkaccess.Service{Store: frameworkaccess.RequestStore(ctx, s)}
	if service.Entitled(ctx, 7) {
		t.Fatal("store failure admitted entitlement")
	}
	s.entitlementErr = nil
	if !service.Entitled(ctx, 7) || s.entitlementCalls != 2 {
		t.Fatal("store failure was cached in the request")
	}
}

func TestFrameworkAccess_RequestSnapshot(t *testing.T) {
	s := &frameworkAccessStore{}
	ctx := frameworkaccess.RequestContext(t.Context())
	service := frameworkaccess.Service{Store: frameworkaccess.RequestStore(ctx, s), Frameworks: []string{"fw-alpha"}}
	if !service.Entitled(ctx, 7) {
		t.Fatal("live entitlement missing")
	}
	s.entitlementErr = errors.New("invented entitlement failure")
	caller, err := frameworkCaller(s)(ctx, 7, []string{"fw-alpha"})
	if err != nil || caller.AccessUnavailable || !caller.Readable("fw-alpha", "licensed") || s.entitlementCalls != 1 {
		t.Fatal("request did not reuse the entitlement snapshot")
	}
	next := frameworkaccess.RequestContext(t.Context())
	service.Store = frameworkaccess.RequestStore(next, s)
	if service.Entitled(next, 7) || s.entitlementCalls != 2 {
		t.Fatal("next request reused the earlier entitlement snapshot")
	}
}

func TestFrameworkAccess_OnlyEntitlementFailureFallsBack(t *testing.T) {
	s := &frameworkAccessStore{entitlementErr: errors.New("invented entitlement failure")}
	access := frameworkCaller(s)
	caller, err := access(t.Context(), 7, []string{"fw-alpha", "fw-beta"})
	if err != nil || !caller.AccessUnavailable || caller.Readable("fw-alpha", "licensed") || !caller.Readable("fw-beta", "free") {
		t.Fatalf("incorrect entitlement fallback: %v", err)
	}
	s.entitlementErr = nil
	caller, err = access(t.Context(), 7, []string{"fw-alpha", "fw-beta"})
	if err != nil || caller.AccessUnavailable || !caller.Readable("fw-alpha", "licensed") || caller.Readable("fw-unknown", "free") || s.entitlementCalls != 2 {
		t.Fatalf("access was cached or incorrectly projected: %v", err)
	}
	s.accountErr = errors.New("invented account store failure")
	caller, err = access(t.Context(), 7, []string{"fw-alpha"})
	if err != nil || !caller.AccessUnavailable || caller.Readable("fw-alpha", "licensed") || s.entitlementCalls != 2 {
		t.Fatal("combined store failure did not fail to the reduced set")
	}
}
