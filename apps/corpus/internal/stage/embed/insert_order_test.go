package embed

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type orderedDB struct{ hashes []string }

func (d *orderedDB) Exec(_ context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
	if strings.Contains(query, "name: RepairEmbedRow ") {
		d.hashes = append(d.hashes, args[4].(string))
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	d.hashes = append(d.hashes, args[1].(string))
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (*orderedDB) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	panic("unexpected query")
}
func (*orderedDB) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	panic("unexpected query")
}

func TestWriteRows_OrdersInsertsAndRepairsByInputHash(t *testing.T) {
	rows := []Row{{InputSHA256: "d"}, {InputSHA256: "b"}, {InputSHA256: "a"}, {InputSHA256: "c"}, {InputSHA256: "a"}}
	incumbents := map[string]incumbentRow{"b": {stale: true}, "c": {}}
	db := &orderedDB{}
	if _, err := writeRows(context.Background(), store.New(db), "recipe", 1, rows, incumbents); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(db.hashes, []string{"a", "b", "d"}) {
		t.Fatalf("insertion order=%v", db.hashes)
	}
	if rows[0].InputSHA256 != "d" {
		t.Fatal("mutated artifact row order")
	}
}
