package retrieval

import (
	"context"
	"fmt"
	"slices"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
	"github.com/jackc/pgx/v5"
)

const changesSetSQL = `SELECT version,CASE WHEN ok THEN country END,CASE WHEN ok THEN release END,CASE WHEN ok THEN predecessor END,CASE WHEN ok THEN predecessor_identity_hash END,baseline,document_added,document_removed,document_validity,provision_validity,provision_text,document_key,CASE WHEN ok THEN digest END,count(*) OVER (),ok FROM (SELECT *, (octet_length(country)<=2 AND octet_length(release)<=11 AND coalesce(octet_length(predecessor),0)<=11 AND coalesce(octet_length(predecessor_identity_hash),0)<=64 AND octet_length(digest)<=64 AND 8::bigint+octet_length(version::text)+octet_length(country)+octet_length(release)+coalesce(octet_length(predecessor),0)+coalesce(octet_length(predecessor_identity_hash),0)<=$1) AS ok FROM release_change_set) bounded`
const changesSeekKind = `SELECT ordinal FROM release_change WHERE kind=$1 AND ordinal>$2 ORDER BY ordinal LIMIT $3`
const changesSeekKey = `SELECT ordinal FROM release_change WHERE kind=$1 AND key=$2 AND ordinal>$3 ORDER BY ordinal LIMIT $4`
const changesSeekKey2 = `SELECT ordinal FROM release_change WHERE kind=$1 AND key2=$2 AND ordinal>$3 ORDER BY ordinal LIMIT $4`
const changesLoad = `SELECT ordinal,CASE WHEN ok THEN kind END,CASE WHEN ok THEN key END,CASE WHEN ok THEN key2 END,CASE WHEN ok THEN payload END FROM (SELECT *,octet_length(kind)<=18 AND coalesce(char_length(key),0)<=$2 AND coalesce(char_length(key2),0)<=$2 AND octet_length(payload)<=$3 AS ok FROM release_change WHERE ordinal=ANY($1::bigint[])) bounded ORDER BY ordinal`

func changesSet(ctx context.Context, tx pgx.Tx, result ChangesResult) (ChangesResult, error) {
	var header releasechanges.Header
	var digest, country, rel *string
	var counts [6]int64
	var n int
	var ok bool
	err := tx.QueryRow(ctx, changesSetSQL, corpusdb.ReleaseChangeLimits().TotalBytes).Scan(&header.Version, &country, &rel, &header.Predecessor, &header.PredecessorIdentityHash, &header.Baseline, &counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &digest, &n, &ok)
	if err != nil {
		return ChangesResult{}, databaseFailure("read changes set", err)
	}
	if !ok || country == nil || rel == nil || digest == nil {
		return ChangesResult{}, changeInvalid("set exceeds limits")
	}
	header.Country, header.Release = *country, *rel
	if _, err = releasechanges.NewHasher(header); err != nil || n != 1 || header.Country != result.Country || header.Release != result.Release || !validHash(*digest) || header.Predecessor != nil && !validChangesRelease(*header.Predecessor) || !header.Baseline && header.Predecessor == nil || header.Baseline != (header.PredecessorIdentityHash == nil) {
		return ChangesResult{}, ErrUnavailable
	}
	kinds := []string{"document_added", "document_removed", "document_validity", "provision_validity", "provision_text", "document_key"}
	result.PreviousRelease = header.Predecessor
	result.State = "available"
	result.Counts = make(map[string]int64, len(kinds))
	for i, kind := range kinds {
		if counts[i] < 0 || header.Baseline && counts[i] != 0 {
			return ChangesResult{}, ErrUnavailable
		}
		result.Counts[kind] = counts[i]
	}
	if header.Baseline {
		result.State = "baseline"
		result.Counts = nil
	}
	return result, nil
}

func queryChanges(ctx context.Context, tx pgx.Tx, req ChangesRequest, cursor changesCursor, total int64) ([]releasechanges.Row, error) {
	var ordinals []int64
	for _, kind := range req.Kinds {
		statements := []string{changesSeekKind}
		if req.Document != "" {
			statements = []string{changesSeekKey, changesSeekKey2}
		}
		for _, stmt := range statements {
			args := []any{kind, cursor.LastOrdinal, cursor.FirstLimit + 1}
			if req.Document != "" {
				args = []any{kind, req.Document, cursor.LastOrdinal, cursor.FirstLimit + 1}
			}
			rows, err := tx.Query(ctx, stmt, args...)
			if err != nil {
				return nil, databaseFailure("seek changes", err)
			}
			for rows.Next() {
				var ordinal int64
				if err = rows.Scan(&ordinal); err != nil {
					rows.Close()
					return nil, databaseFailure("scan changes ordinal", err)
				}
				ordinals = append(ordinals, ordinal)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, databaseFailure("read changes ordinals", err)
			}
		}
	}
	slices.Sort(ordinals)
	ordinals = slices.Compact(ordinals)
	if len(ordinals) > cursor.FirstLimit+1 {
		ordinals = ordinals[:cursor.FirstLimit+1]
	}
	if len(ordinals) == 0 {
		return nil, nil
	}
	limits := corpusdb.ReleaseChangeLimits()
	rows, err := tx.Query(ctx, changesLoad, ordinals, limits.KeyCharacters, limits.PayloadBytes)
	if err != nil {
		return nil, databaseFailure("load changes", err)
	}
	defer rows.Close()
	var result []releasechanges.Row
	for rows.Next() {
		var row releasechanges.Row
		var kind, payload *string
		if err = rows.Scan(&row.Ordinal, &kind, &row.Key, &row.Key2, &payload); err != nil {
			return nil, databaseFailure("scan changes", err)
		}
		if row.Ordinal < 1 || row.Ordinal > total {
			return nil, changeInvalid("ordinal outside counts")
		}
		if kind == nil || payload == nil || row.Key == nil || !slices.Contains(req.Kinds, *kind) {
			return nil, ErrUnavailable
		}
		row.Kind = *kind
		row.Payload = *payload
		if _, err = validateChangePayload(row); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, databaseFailure("read changes", err)
	}
	if len(result) != len(ordinals) {
		return nil, fmt.Errorf("missing changes row: %w", ErrUnavailable)
	}
	return result, nil
}
