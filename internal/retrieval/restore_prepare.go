package retrieval

import (
	"context"
	"strconv"
	"time"

	"danny.vn/trachuan/internal/retrieval/lexical"
	"danny.vn/trachuan/internal/retrieval/restore"
	"github.com/jackc/pgx/v5"
)

const restorationSQL = `SELECT left(c.content,4096), char_length(c.content)>4096 FROM chunk c JOIN document d ON d.id=c.document_id WHERE ` + eligibility + ` AND d.class <> 'relation_context' AND -(c.sparse <#> $1::sparsevec) > 0 ORDER BY -(c.sparse <#> $1::sparsevec) DESC,c.id LIMIT $5`

func rollbackRestoration(ctx context.Context, tx rollbacker) {
	deadline, ok := ctx.Deadline()
	if ok {
		if existing, exists := ctx.Value(rollbackDeadlineKey{}).(time.Time); exists && existing.Before(deadline) {
			deadline = existing
		}
		ctx = WithRollbackDeadline(ctx, deadline)
	}
	rollback(ctx, tx)
}

func (e *Engine) restoreQuery(ctx context.Context, p Prepared) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, e.deadline())
	defer cancel()
	vector, err := lexical.Query(p.Request.Query, e.release.LexicalRecipe)
	if err != nil {
		return "", false, databaseFailure("encode restoration query failed", err)
	}
	if err = ctx.Err(); err != nil {
		return "", false, databaseFailure("restoration timed out", err)
	}
	if len(vector) == 0 {
		return p.Request.Query, false, nil
	}
	tx, err := e.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return "", false, databaseFailure("begin restoration failed", err)
	}
	defer rollbackRestoration(ctx, tx)
	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline).Milliseconds()
	if remaining < 1 {
		return "", false, databaseFailure("restoration timed out", context.DeadlineExceeded)
	}
	if err = setLocal(ctx, tx, "statement_timeout", strconv.FormatInt(remaining, 10)); err != nil {
		return "", false, err
	}
	date := p.collectionDate
	if date == "" {
		date = e.searchDate(p.Request)
	}
	rows, err := tx.Query(ctx, restorationSQL, sparseVector(vector, e.release.LexicalRecipe.Dimensions), date, p.Request.AsOf != nil, true, 20)
	if err != nil {
		return "", false, databaseFailure("restoration query failed", err)
	}
	var candidates []restore.Candidate
	for rows.Next() {
		var c restore.Candidate
		if err = rows.Scan(&c.Text, &c.PrefixCut); err != nil {
			rows.Close()
			return "", false, databaseFailure("read restoration candidate failed", err)
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", false, databaseFailure("read restoration candidates failed", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", false, databaseFailure("commit restoration failed", err)
	}
	output, changed, err := restore.Restore(ctx, []byte(p.Request.Query), candidates, 8)
	if err != nil {
		return "", false, databaseFailure("restoration failed", err)
	}
	if err = ctx.Err(); err != nil {
		return "", false, databaseFailure("restoration timed out", err)
	}
	return string(output), changed, nil
}
