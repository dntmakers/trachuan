//go:build integration

package corpusdb

import (
	"context"
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
	"github.com/jackc/pgx/v5"
)

type boundedObserver struct {
	conn    *pgx.Conn
	exposed bool
}

func (o *boundedObserver) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := o.conn.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return observedTx{Tx: tx, observer: o}, nil
}

type observedTx struct {
	pgx.Tx
	observer *boundedObserver
}

func (t observedTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := t.Tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return observedRows{Rows: rows, observer: t.observer}, nil
}

type observedRows struct {
	pgx.Rows
	observer *boundedObserver
}

func (r observedRows) Next() bool {
	if !r.Rows.Next() {
		return false
	}
	values, err := r.Rows.Values()
	if err == nil && len(values) == 6 && values[1] != nil && values[5] == false {
		r.observer.exposed = true
	}
	return true
}
func TestReleaseChanges_OversizedKindNotSent(t *testing.T) {
	ctx := context.Background()
	conn := schemaDB(t)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteReleaseChanges(ctx, tx, releasechanges.Header{Version: 1, Country: "vn", Release: "r"}, []releasechanges.Row{{Ordinal: 1, Kind: "document_added", Payload: `{}`}}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `ALTER TABLE release_change DROP CONSTRAINT release_change_kind_check; UPDATE release_change SET kind=repeat('x',10001)`); err != nil {
		t.Fatal(err)
	}
	observer := &boundedObserver{conn: conn}
	if _, err = ReadReleaseChanges(ctx, observer, ProjectionLimits{KeyCharacters: 100, PayloadBytes: 1000, TotalBytes: 10000}, nil); err == nil {
		t.Fatal("accepted oversized kind")
	}
	if observer.exposed {
		t.Fatal("oversized kind reached the reader")
	}
}
