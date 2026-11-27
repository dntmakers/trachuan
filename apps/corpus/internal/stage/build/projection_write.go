package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
	"github.com/jackc/pgx/v5"
)

type projectionBudget struct {
	used  int64
	limit int64
}

func (b *projectionBudget) Write(p []byte) (int, error) {
	if int64(len(p)) > b.limit-b.used {
		return 0, fmt.Errorf("projection exceeds byte limit")
	}
	b.used += int64(len(p))
	return len(p), nil
}

func ProjectionHeader(run stage.Run) (releasechanges.Header, error) {
	lock := run.Snapshot.LockedPredecessor
	if lock == nil {
		return releasechanges.Header{}, fmt.Errorf("missing locked predecessor")
	}
	h := releasechanges.Header{Version: releasechanges.Version, Country: run.Country, Release: run.Rel, Baseline: lock.IdentitySHA256 == stage.NoPreviousReport}
	if lock.Release != stage.NoPreviousReport {
		value := lock.Release
		h.Predecessor = &value
	}
	if !h.Baseline {
		value := lock.IdentitySHA256
		h.PredecessorIdentityHash = &value
	}
	return h, nil
}

func writeChangesProjection(ctx context.Context, conn *pgx.Conn, env stage.Env, run stage.Run, inv Inventory, identityInventoryHash string) (corpusdb.ProjectionResult, error) {
	header, err := ProjectionHeader(run)
	if err != nil {
		return corpusdb.ProjectionResult{}, err
	}
	decision := run.Snapshot.PreviousReport
	if decision == nil {
		decision = &stage.PreviousReport{SHA256: stage.NoPreviousReport}
	}
	previous, err := releasechange.ResolvePredecessor(ctx, store.New(env.DB), env.Artifacts, run, *decision, run.Snapshot.LockedPredecessor.Release)
	if err != nil {
		return corpusdb.ProjectionResult{}, fmt.Errorf("resolve projection predecessor: %w", err)
	}
	lock := run.Snapshot.LockedPredecessor
	if decision.SHA256 != lock.ReportSHA256 || previous.Release != lock.Release || previous.IdentitySHA256 != lock.IdentitySHA256 {
		return corpusdb.ProjectionResult{}, fmt.Errorf("projection predecessor differs from lock")
	}
	members, err := ProjectionMembers(inv, identityInventoryHash)
	if err != nil {
		return corpusdb.ProjectionResult{}, fmt.Errorf("projection members: %w", err)
	}
	if err = os.MkdirAll(filepath.Join(env.Artifacts.Root, ".tmp"), 0700); err != nil {
		return corpusdb.ProjectionResult{}, fmt.Errorf("create projection temp directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Join(env.Artifacts.Root, ".tmp"), "projection-identity-")
	if err != nil {
		return corpusdb.ProjectionResult{}, fmt.Errorf("create projection identity: %w", err)
	}
	path := file.Name()
	defer func() { _ = os.Remove(path) }()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		_ = file.Close()
		return corpusdb.ProjectionResult{}, fmt.Errorf("begin projection transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = releasechange.WriteIdentity(ctx, file, releasechange.NewSQLRelease(tx), members, releasechange.IdentityBindings{Rel: run.Rel})
	closeErr := file.Close()
	if err != nil {
		return corpusdb.ProjectionResult{}, fmt.Errorf("write projection identity: %w", err)
	}
	if closeErr != nil {
		return corpusdb.ProjectionResult{}, fmt.Errorf("close projection identity: %w", closeErr)
	}
	identityHash := stage.NoPreviousReport
	if !header.Baseline {
		identityHash = "sha256:" + lock.IdentitySHA256
	}
	input := releasechange.ChangeInputs{Current: path, Predecessor: previous.IdentityPath, Members: members, Bindings: releasechange.ChangesBindings{PredecessorRel: lock.Release, PredecessorIdentitySHA256: identityHash}}
	bounded, err := releasechanges.NewWriter(header, &projectionBudget{limit: corpusdb.ReleaseChangeLimits().TotalBytes})
	if err != nil {
		return corpusdb.ProjectionResult{}, fmt.Errorf("projection budget: %w", err)
	}
	result, err := corpusdb.WriteReleaseChangesStream(ctx, tx, header, func(emit func(releasechanges.Row) error) error {
		return releasechange.ProjectRecords(header.Baseline, func(yield func(any) error) error { return releasechange.Generate(input, yield) }, func(row releasechanges.Row) error {
			if addErr := bounded.Add(row); addErr != nil {
				return addErr
			}
			return emit(row)
		})
	})
	if err != nil {
		return corpusdb.ProjectionResult{}, fmt.Errorf("write release change projection: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return corpusdb.ProjectionResult{}, fmt.Errorf("commit projection: %w", err)
	}
	return result, nil
}
