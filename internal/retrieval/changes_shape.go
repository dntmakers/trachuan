package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
	"github.com/jackc/pgx/v5"
)

func shapeChange(ctx context.Context, tx pgx.Tx, row releasechanges.Row) (json.RawMessage, error) {
	fields, err := validateChangePayload(row)
	if err != nil {
		return nil, err
	}
	var key, anchor, change string
	_ = json.Unmarshal(fields["key"], &key)
	fields["kind"], _ = json.Marshal(row.Kind)
	readable := row.Kind != "document_removed"
	if row.Kind == "provision_text" {
		if json.Unmarshal(fields["anchor"], &anchor) != nil || anchor == "" || json.Unmarshal(fields["change"], &change) != nil {
			return nil, ErrUnavailable
		}
		readable = change != "removed"
		var ambiguous bool
		if json.Unmarshal(fields["ambiguous"], &ambiguous) != nil {
			return nil, ErrUnavailable
		}
		readable = readable && !ambiguous
	}
	handle := key
	if anchor != "" {
		handle += "#" + anchor
	}
	if readable && utf8.RuneCountInString(handle) <= corpusdb.ReleaseChangeLimits().KeyCharacters {
		var exists bool
		if anchor == "" {
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM document WHERE key=$1)`, key).Scan(&exists)
		} else {
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provision p JOIN document d ON d.id=p.document_id WHERE d.key=$1 AND p.anchor=$2)`, key, anchor).Scan(&exists)
		}
		if err != nil {
			return nil, databaseFailure("lookup changes handle", err)
		}
		if exists {
			fields["handle"], _ = json.Marshal(handle)
		}
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode change: %w", err)
	}
	if len(out) > corpusdb.ReleaseChangeLimits().PayloadBytes {
		return nil, ErrUnavailable
	}
	return out, nil
}
