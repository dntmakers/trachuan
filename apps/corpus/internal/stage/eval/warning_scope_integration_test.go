//go:build integration

package eval

import (
	"context"
	"testing"

	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWarningScope_StoredParentLinks(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testpg.NewDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `CREATE TABLE document (id bigint PRIMARY KEY, key text NOT NULL); CREATE TABLE provision (id bigint PRIMARY KEY, document_id bigint NOT NULL, parent_id bigint, anchor text NOT NULL); INSERT INTO document VALUES (1,'vn:1'),(2,'vn:2'); INSERT INTO provision VALUES (1,1,NULL,'chuong-1'),(2,1,1,'dieu-5'),(3,1,2,'dieu-5/khoan-1'),(4,1,1,'dieu-6'),(5,2,NULL,'dieu-30')`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		carrier string
		want    bool
	}{{"vn:1#chuong-1", true}, {"vn:1#dieu-5", true}, {"vn:1#dieu-5/khoan-1", true}, {"vn:1#dieu-6", false}, {"vn:2#dieu-30", false}, {"vn:1#missing", false}} {
		got, err := warningScope(ctx, pool, "vn:1#dieu-5", tc.carrier)
		if err != nil || got != tc.want {
			t.Fatalf("%s: %v, %v", tc.carrier, got, err)
		}
	}
}
