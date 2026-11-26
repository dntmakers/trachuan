package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/jackc/pgx/v5"
)

type ChangesResult struct {
	Country         string            `json:"country"`
	Release         string            `json:"release"`
	State           string            `json:"state"`
	PreviousRelease *string           `json:"previous_release,omitempty"`
	Counts          map[string]int64  `json:"counts,omitempty"`
	Items           []json.RawMessage `json:"items"`
	NextCursor      string            `json:"next_cursor,omitempty"`
}

func (e *Engine) Changes(ctx context.Context, req ChangesRequest) (ChangesResult, error) {
	req, cursor, err := normalizeChangesRequest(req)
	if err != nil {
		return ChangesResult{}, err
	}
	if req.Release != "" && req.Release != e.release.Rel || req.Cursor != "" && cursor.Release != e.release.Rel {
		return ChangesResult{}, ErrReleaseExpired
	}
	result := ChangesResult{Country: req.Country, Release: e.release.Rel, State: "unavailable", Items: []json.RawMessage{}}
	if e.release.SchemaVersion < 9 {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, e.deadline())
	defer cancel()
	tx, err := e.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return ChangesResult{}, databaseFailure("begin changes transaction", err)
	}
	defer rollbackRestoration(ctx, tx)
	if err = setLocal(ctx, tx, "statement_timeout", strconv.FormatInt(e.deadline().Milliseconds(), 10)); err != nil {
		return ChangesResult{}, fmt.Errorf("set changes deadline: %w", err)
	}
	result, err = changesSet(ctx, tx, result)
	if err != nil {
		return ChangesResult{}, err
	}
	var total int64
	for _, count := range result.Counts {
		if count > math.MaxInt64-total {
			return ChangesResult{}, ErrUnavailable
		}
		total += count
	}
	if req.Cursor != "" && cursor.LastOrdinal > total {
		return ChangesResult{}, ErrInvalidRequest
	}
	if result.State == "available" {
		cursor.Release = e.release.Rel
		rows, queryErr := queryChanges(ctx, tx, req, cursor, total)
		if queryErr != nil {
			return ChangesResult{}, queryErr
		}
		for i, row := range rows {
			if i >= cursor.FirstLimit {
				break
			}
			item, shapeErr := shapeChange(ctx, tx, row)
			if shapeErr != nil {
				return ChangesResult{}, shapeErr
			}
			candidate := result
			candidate.Items = append(append([]json.RawMessage(nil), result.Items...), item)
			next := cursor
			next.LastOrdinal = row.Ordinal
			candidate.NextCursor = encodeChangesCursor(next)
			raw, marshalErr := json.Marshal(candidate)
			if marshalErr != nil {
				return ChangesResult{}, fmt.Errorf("marshal changes: %w", marshalErr)
			}
			if len(raw) > 32768 {
				if len(result.Items) == 0 {
					return ChangesResult{}, ErrUnavailable
				}
				break
			}
			result.Items = candidate.Items
		}
		if len(result.Items) < len(rows) && len(result.Items) > 0 {
			cursor.LastOrdinal = rows[len(result.Items)-1].Ordinal
			result.NextCursor = encodeChangesCursor(cursor)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return ChangesResult{}, databaseFailure("commit changes transaction", err)
	}
	return result, nil
}
