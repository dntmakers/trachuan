package corpusdb

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type projectionBeginFake struct{ err error }

func (f projectionBeginFake) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return projectionTxFake{err: f.err}, nil
}

type projectionTxFake struct {
	pgx.Tx
	err error
}

func (f projectionTxFake) QueryRow(context.Context, string, ...any) pgx.Row {
	return projectionRowFake{err: f.err}
}
func (projectionTxFake) Rollback(context.Context) error { return nil }

type projectionRowFake struct{ err error }

func (f projectionRowFake) Scan(...any) error { return f.err }

func TestReadReleaseChanges_ClassifiesMissingAndInfrastructure(t *testing.T) {
	transient := errors.New("connection lost")
	for _, tc := range []struct {
		err     error
		invalid bool
	}{{pgx.ErrNoRows, true}, {transient, false}} {
		_, err := ReadReleaseChanges(context.Background(), projectionBeginFake{err: tc.err}, ReleaseChangeLimits(), nil)
		var invalid *InvalidProjectionError
		if errors.As(err, &invalid) != tc.invalid || !errors.Is(err, tc.err) {
			t.Fatalf("input=%v error=%v", tc.err, err)
		}
	}
}
