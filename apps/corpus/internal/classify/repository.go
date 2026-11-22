package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const submissionLedgerLockKey int64 = 861785302

type PGRepository struct {
	Pool *pgxpool.Pool
	conn *pgxpool.Conn
	tx   pgx.Tx
}

func (p *PGRepository) Lock(ctx context.Context) (func(), error) {
	if p.conn != nil {
		return nil, fmt.Errorf("submission ledger already locked")
	}
	conn, err := p.Pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire submission ledger connection: %w", err)
	}
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, submissionLedgerLockKey); err != nil {
		discardLedgerConnection(conn)
		return nil, fmt.Errorf("lock submission ledger: %w", err)
	}
	p.conn = conn
	return func() {
		p.conn = nil
		releaseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var unlocked bool
		if unlockErr := conn.QueryRow(releaseCtx, `SELECT pg_advisory_unlock($1)`, submissionLedgerLockKey).Scan(&unlocked); unlockErr != nil || !unlocked {
			discardLedgerConnection(conn)
			return
		}
		conn.Release()
	}, nil
}

func discardLedgerConnection(conn *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = conn.Hijack().Close(ctx)
}

func (p *PGRepository) Admission(ctx context.Context, fn func() error) error {
	if p.conn == nil || p.tx != nil {
		return fmt.Errorf("submission ledger lock required for admission")
	}
	tx, err := p.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin classification admission: %w", err)
	}
	p.tx = tx
	defer func() {
		p.tx = nil
		_ = tx.Rollback(context.Background())
	}()
	if err = fn(); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit classification admission: %w", err)
	}
	return nil
}

func (p *PGRepository) queries() *store.Queries {
	if p.tx != nil {
		return store.New(p.tx)
	}
	return store.New(p.conn)
}

func (p *PGRepository) List(ctx context.Context) ([]Submission, error) {
	if p.conn == nil {
		return nil, fmt.Errorf("submission ledger lock required")
	}
	rows, err := p.queries().ListClassificationSubmissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("read submission ledger: %w", err)
	}
	var result []Submission
	for _, v := range rows {
		s := Submission{KeyVersion: int(v.KeyVersion), ID: v.ID, RunID: v.RunID, Country: v.Country, Release: v.Rel, ScreenHash: v.ScreenHash, ItemsHash: v.ItemListHash, State: v.State, CreatedAt: v.CreatedAt, SubmittedAt: v.SubmittedAt, DeadlineAt: v.DeadlineAt, Budget: v.Budget, BudgetMicroUSD: v.BudgetMicroUsd, ReserveMicroUSD: v.ReservedMicroUsd, CostMicroUSD: v.CostMicroUsd, Settled: v.CostSettled}
		if v.PriceVersion != nil {
			s.PriceVersion = *v.PriceVersion
		}
		if v.ProviderJobID != nil {
			s.BatchID = *v.ProviderJobID
		}
		for _, entry := range []struct {
			raw []byte
			dst any
		}{{v.Recipe, &s.Recipe}, {v.Families, &s.Families}, {v.ItemList, &s.Items}, {v.Results, &s.Results}} {
			if err = json.Unmarshal(entry.raw, entry.dst); err != nil {
				return nil, fmt.Errorf("decode submission ledger: %w", err)
			}
		}
		if s.ItemsHash != itemsHash(s.Items) {
			return nil, fmt.Errorf("submission ordered item hash mismatch")
		}
		result = append(result, s)
	}
	return result, nil
}

func (p *PGRepository) Save(ctx context.Context, s Submission) error {
	if p.conn == nil {
		return fmt.Errorf("submission ledger lock required")
	}
	if len(s.attempts) == 0 || p.tx != nil {
		return saveSubmission(ctx, p.queries(), s)
	}
	return p.Admission(ctx, func() error { return saveSubmission(ctx, p.queries(), s) })
}

func (p *PGRepository) Restore(ctx context.Context, submissions []Submission) error {
	if p.conn == nil {
		return fmt.Errorf("submission ledger lock required")
	}
	tx, err := p.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin submission reconstruction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := store.New(tx)
	if err = q.DeleteClassificationAttemptIndex(ctx); err != nil {
		return fmt.Errorf("clear classification attempt index: %w", err)
	}
	for _, s := range submissions {
		if err = saveSubmission(ctx, q, s); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit submission reconstruction: %w", err)
	}
	return nil
}

func saveSubmission(ctx context.Context, q *store.Queries, s Submission) error {
	var recipe, families, items, results []byte
	for _, entry := range []struct {
		src any
		dst *[]byte
	}{{s.Recipe, &recipe}, {s.Families, &families}, {s.Items, &items}, {s.Results, &results}} {
		b, err := json.Marshal(entry.src)
		if err != nil {
			return fmt.Errorf("encode submission ledger: %w", err)
		}
		*entry.dst = b
	}
	var batch *string
	if s.BatchID != "" {
		batch = &s.BatchID
	}
	var version *string
	if s.PriceVersion != "" {
		version = &s.PriceVersion
	}
	err := q.SaveClassificationSubmission(ctx, store.SaveClassificationSubmissionParams{ID: s.ID, RunID: s.RunID, ScreenHash: s.ScreenHash, Recipe: recipe, Families: families, ItemList: items, ItemListHash: s.ItemsHash, ProviderJobID: batch, State: s.State, CreatedAt: s.CreatedAt, SubmittedAt: s.SubmittedAt, DeadlineAt: s.DeadlineAt, Budget: s.Budget, BudgetMicroUsd: s.BudgetMicroUSD, ReservedMicroUsd: s.ReserveMicroUSD, CostMicroUsd: s.CostMicroUSD, CostSettled: s.Settled, Results: results, KeyVersion: int32(s.KeyVersion), PriceVersion: version})
	if err != nil {
		return fmt.Errorf("save submission ledger: %w", err)
	}
	for _, a := range s.attempts {
		if err = q.IndexClassificationAttempt(ctx, store.IndexClassificationAttemptParams{SubmissionID: a.SubmissionID, ItemIndex: int32(a.Index), RequestKey: a.Key, Outcome: a.Outcome, PriceVersion: a.PriceVersion, CostMicroUsd: a.Cost}); err != nil {
			return fmt.Errorf("index classification attempt: %w", err)
		}
	}
	return nil
}
