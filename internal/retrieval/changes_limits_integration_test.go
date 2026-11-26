//go:build integration

package retrieval

import (
	"context"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/corpusdb"
)

func TestChangesLoad_RejectsOversizedFieldsBeforeTransfer(t *testing.T) {
	db, _ := readFixture(t)
	ctx := context.Background()
	readExec(t, db, `INSERT INTO release_change (ordinal,kind,key,key2,payload) VALUES (1,'document_added',$1,$1,'{}')`, strings.Repeat("x", 513))
	limits := corpusdb.ReleaseChangeLimits()
	var ordinal int64
	var kind *string
	var key, key2, payload *string
	err := db.QueryRow(ctx, changesLoad, []int64{1}, limits.KeyCharacters, limits.PayloadBytes).Scan(&ordinal, &kind, &key, &key2, &payload)
	if err != nil || key != nil || key2 != nil || payload != nil {
		t.Fatalf("oversized fields transferred: key=%v key2=%v payload=%v err=%v", key != nil, key2 != nil, payload != nil, err)
	}
	readExec(t, db, `ALTER TABLE release_change DROP CONSTRAINT release_change_kind_check`)
	readExec(t, db, `UPDATE release_change SET kind=$1,key='vn:1',key2=NULL WHERE ordinal=1`, strings.Repeat("x", 10000))
	err = db.QueryRow(ctx, changesLoad, []int64{1}, limits.KeyCharacters, limits.PayloadBytes).Scan(&ordinal, &kind, &key, &key2, &payload)
	if err != nil || kind != nil {
		t.Fatalf("oversized kind transferred=%v err=%v", kind != nil, err)
	}
}
