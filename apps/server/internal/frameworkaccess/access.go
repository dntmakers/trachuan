package frameworkaccess

import (
	"context"
	"errors"
	"sort"

	"danny.vn/trachuan/apps/server/internal/accountemail"
	"danny.vn/trachuan/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

type Store interface {
	FrameworkAccessSnapshot(context.Context, int64) (store.FrameworkAccessSnapshot, error)
}

type Service struct {
	Store      Store
	Frameworks []string
}

type Set struct {
	known, codes map[string]struct{}
	all          bool
}

type Decision string

const (
	Full    Decision = "full"
	Reduced Decision = "reduced"
)

type failure string

func (e failure) Error() string { return string(e) }

const ErrUnavailable failure = "framework access unavailable"

func Decide(code, class string, set Set) Decision {
	if _, ok := set.known[code]; !ok {
		return Reduced
	}
	if class == "free" {
		return Full
	}
	if class == "licensed" {
		if _, ok := set.codes[code]; set.all || ok {
			return Full
		}
	}
	return Reduced
}

func (s Set) Scopes() []string {
	if s.all {
		return []string{"*"}
	}
	scopes := make([]string, 0, len(s.codes))
	for code := range s.codes {
		scopes = append(scopes, code)
	}
	sort.Strings(scopes)
	return scopes
}

func (s *Service) Entitled(ctx context.Context, accountID int64) bool {
	if accountID <= 0 || s.Store == nil {
		return false
	}
	email, rows, err := s.liveEntitlements(ctx, accountID)
	if err != nil || email == "" {
		return false
	}
	for _, row := range rows {
		if row.Email == email && row.RevokedAt == nil {
			return true
		}
	}
	return false
}

func EmptySet(frameworks []string) Set {
	set := Set{known: make(map[string]struct{}, len(frameworks)), codes: make(map[string]struct{})}
	for _, code := range frameworks {
		if _, err := store.ParseFrameworkScope(code); err == nil && code != "*" {
			set.known[code] = struct{}{}
		}
	}
	return set
}

func (s *Service) AccessSet(ctx context.Context, accountID int64) (Set, error) {
	email, rows, err := s.liveEntitlements(ctx, accountID)
	if err != nil || email == "" {
		return Set{}, err
	}
	set := EmptySet(s.Frameworks)
	for _, row := range rows {
		if row.Email != email || row.RevokedAt != nil {
			continue
		}
		if row.Scope == "*" {
			set.all = true
			continue
		}
		if _, ok := set.known[row.Scope]; ok {
			set.codes[row.Scope] = struct{}{}
		}
	}
	return set, nil
}

func (s *Service) liveEntitlements(ctx context.Context, accountID int64) (string, []store.FrameworkEntitlement, error) {
	snapshot, err := s.Store.FrameworkAccessSnapshot(ctx, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, ErrUnavailable
	}
	account := snapshot.Account
	if account.BlockedAt != nil || account.DeleteRequestedAt != nil {
		return "", nil, nil
	}
	email, err := accountemail.Parse(account.Email)
	if err != nil {
		return "", nil, ErrUnavailable
	}
	return email, snapshot.Entitlements, nil
}
