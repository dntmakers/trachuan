package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type FrameworkAccessSnapshot struct {
	Account      Account
	Entitlements []FrameworkEntitlement
}

func (q *Queries) FrameworkAccessSnapshot(ctx context.Context, id int64) (FrameworkAccessSnapshot, error) {
	rows, err := q.db.Query(ctx, `WITH a AS (
 SELECT id, email, blocked_at, delete_requested_at,
 btrim(email, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000') AS trimmed_email
 FROM account WHERE id = $1
)
SELECT a.id, a.email, a.blocked_at, a.delete_requested_at, e.email, e.scope
FROM a LEFT JOIN framework_entitlement e ON e.email = split_part(a.trimmed_email, '@', 1) || '@' || lower(split_part(a.trimmed_email, '@', 2) COLLATE "C") AND e.revoked_at IS NULL
ORDER BY e.scope, e.id`, id)
	if err != nil {
		return FrameworkAccessSnapshot{}, err
	}
	defer rows.Close()
	var result FrameworkAccessSnapshot
	found := false
	for rows.Next() {
		var email, scope *string
		if err := rows.Scan(&result.Account.ID, &result.Account.Email, &result.Account.BlockedAt, &result.Account.DeleteRequestedAt, &email, &scope); err != nil {
			return FrameworkAccessSnapshot{}, err
		}
		found = true
		if email != nil && scope != nil {
			result.Entitlements = append(result.Entitlements, FrameworkEntitlement{Email: *email, Scope: *scope})
		}
	}
	if err := rows.Err(); err != nil {
		return FrameworkAccessSnapshot{}, err
	}
	if !found {
		return FrameworkAccessSnapshot{}, pgx.ErrNoRows
	}
	return result, nil
}
