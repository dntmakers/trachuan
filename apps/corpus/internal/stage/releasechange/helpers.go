package releasechange

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/stage/membership"
	"github.com/jackc/pgx/v5"
)

const hashPrefix = "sha256:"
const noneValue = "none"

type InputError struct{ Reason string }

func (e *InputError) Error() string { return e.Reason }

func refuse(format string, args ...any) error {
	return &InputError{Reason: fmt.Sprintf(format, args...)}
}

func sha(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type SQLRelease struct{ q queryer }

func NewSQLRelease(q queryer) SQLRelease { return SQLRelease{q: q} }

func NewMembers(entries []membership.Entry, actualHash, expectedHash string) (Members, error) {
	if actualHash != expectedHash {
		return Members{}, refuse("membership inventory differs from the build's")
	}
	m := Members{active: map[string]bool{}, disposition: map[string]string{}, replacement: map[string]string{}}
	for _, e := range entries {
		m.disposition[e.DocumentKey] = e.Disposition
		if e.Disposition == "active" {
			m.active[e.DocumentKey] = true
		} else if e.Disposition == "source_replaced" && e.ReplacementKey != nil {
			m.replacement[e.DocumentKey] = *e.ReplacementKey
		}
	}
	return m, nil
}

func (m Members) IsActive(key string) bool { return m.active[key] }

func (m Members) Disposition(key string) string { return m.disposition[key] }

func (m Members) Replacement(key string) (string, bool) {
	value, ok := m.replacement[key]
	return value, ok
}

func (m Members) Entries() []membership.Entry {
	entries := make([]membership.Entry, 0, len(m.disposition))
	for key, disposition := range m.disposition {
		entry := membership.Entry{DocumentKey: key, Disposition: disposition}
		if replacement, ok := m.replacement[key]; ok {
			entry.ReplacementKey = &replacement
		}
		entries = append(entries, entry)
	}
	return entries
}
