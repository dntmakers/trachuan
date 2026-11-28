package testdb

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPruneSelection(t *testing.T) {
	const current = "corpus_tpl_0123456789ab"
	tests := []struct {
		name     string
		sessions int
		want     string
	}{
		{"corpus_tpl_abcdef012345", 0, ""},
		{current, 0, "current migration"},
		{"corpus_tpl_abcdef012345", 1, "connected sessions"},
		{"corpus_tpl_ABCDEF012345", 0, "invalid name"},
		{"corpus_tpl_abcdef01234", 0, "invalid name"},
		{"corpus_tpl_abcdef012345x", 0, "invalid name"},
		{"other_corpus_tpl_abcdef012345", 0, "invalid name"},
		{"corpus_tpl_abcdef01234g", 0, "invalid name"},
	}
	for _, tt := range tests {
		t.Run(tt.name+tt.want, func(t *testing.T) {
			if got := pruneSkipReason(tt.name, current, tt.sessions); got != tt.want {
				t.Errorf("skip reason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTestDatabaseName_ReservedNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"corpus_vn_20991215", true},
		{"frameworks_20991215_3", true},
		{"corpus_vn_20261215", false},
		{"corpus_vn_20991215_extra", false},
		{"corpus_tmp_vn_20991215", true},
		{"test_template_20991215", false},
	} {
		if got := testDatabaseName(tc.name); got != tc.want {
			t.Errorf("testDatabaseName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBuilderRoleSkipReason(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		ownsDatabase, connected bool
		want                    string
	}{
		{"idle", false, false, ""},
		{"owner", true, false, "owns a database or has connected sessions"},
		{"connected", false, true, "owns a database or has connected sessions"},
		{"both", true, true, "owns a database or has connected sessions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := builderRoleSkipReason(tc.ownsDatabase, tc.connected); got != tc.want {
				t.Errorf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}

type busyTemplateDrop struct {
	calls int
}

func (d *busyTemplateDrop) Exec(_ context.Context, statement string, _ ...any) (pgconn.CommandTag, error) {
	d.calls++
	if statement != `DROP DATABASE "corpus_tpl_abcdef012345"` {
		return pgconn.CommandTag{}, errors.New("unexpected statement")
	}
	return pgconn.CommandTag{}, &pgconn.PgError{Code: "55006"}
}

func TestDropTemplate_DoesNotRetryBusyDatabase(t *testing.T) {
	conn := &busyTemplateDrop{}
	if err := dropTemplate(context.Background(), conn, "corpus_tpl_abcdef012345"); err == nil || conn.calls != 1 {
		t.Fatalf("calls=%d error=%v", conn.calls, err)
	}
}

type cancelDuringTemplateDrop struct {
	cancel       context.CancelFunc
	closed       bool
	template     bool
	exists       bool
	dropSucceeds bool
}

func (f *cancelDuringTemplateDrop) Exec(ctx context.Context, statement string, _ ...any) (pgconn.CommandTag, error) {
	if f.closed {
		return pgconn.CommandTag{}, errors.New("connection closed")
	}
	if _, ok := ctx.Deadline(); !ok {
		return pgconn.CommandTag{}, errors.New("statement context has no deadline")
	}
	switch statement {
	case `DROP DATABASE "corpus_tpl_abcdef012345"`:
		f.cancel()
		if err := ctx.Err(); err != nil {
			f.closed = true
			return pgconn.CommandTag{}, err
		}
		if f.dropSucceeds {
			f.exists = false
			return pgconn.CommandTag{}, nil
		}
		return pgconn.CommandTag{}, &pgconn.PgError{Code: "55006"}
	case `ALTER DATABASE "corpus_tpl_abcdef012345" IS_TEMPLATE false`:
		f.template = false
		return pgconn.CommandTag{}, nil
	case `ALTER DATABASE "corpus_tpl_abcdef012345" IS_TEMPLATE true`:
		f.template = true
		return pgconn.CommandTag{}, nil
	default:
		return pgconn.CommandTag{}, errors.New("unexpected statement")
	}
}

func TestDropTemplate_CancellationPreservesTemplate(t *testing.T) {
	for _, succeeds := range []bool{false, true} {
		t.Run(fmt.Sprintf("drop_succeeds_%t", succeeds), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			conn := &cancelDuringTemplateDrop{cancel: cancel, template: true, exists: true, dropSucceeds: succeeds}
			err := dropTemplatePreservingFlag(ctx, conn, "corpus_tpl_abcdef012345", true)
			if ctx.Err() != context.Canceled {
				t.Fatal("drop did not cancel the caller")
			}
			if conn.exists && !conn.template {
				t.Fatalf("template remains unmarked after cancellation: drop error = %v", err)
			}
			if !succeeds && err == nil {
				t.Fatal("busy drop returned no error")
			}
			if succeeds && (err != nil || conn.exists) {
				t.Fatalf("successful drop: exists=%t error=%v", conn.exists, err)
			}
		})
	}
}
