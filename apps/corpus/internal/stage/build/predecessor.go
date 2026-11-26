package build

import (
	"context"
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"github.com/jackc/pgx/v5"
)

func predecessorInputHash(input string, lock stage.LockedPredecessor) string {
	return stage.RecipeHash("build-input", "5", input, lock.Release, lock.IdentitySHA256)
}

func (s *Stage) PlanInTransaction(ctx context.Context, env stage.Env, tx pgx.Tx, run stage.Run) ([]stage.Item, error) {
	decision := run.Snapshot.PreviousReport
	if decision == nil {
		if !stage.PredatesDecision(run.Country, run.Rel, run.Snapshot) {
			return nil, fmt.Errorf("run has no previous report decision")
		}
		decision = &stage.PreviousReport{SHA256: stage.NoPreviousReport}
	}
	rel, err := releasechange.FrozenReportRelease(env.Artifacts, *decision)
	if err != nil {
		return nil, fmt.Errorf("resolve frozen report: %w", err)
	}
	resolved, err := releasechange.ResolvePredecessor(ctx, store.New(tx), env.Artifacts, run, *decision, rel)
	if err != nil {
		return nil, fmt.Errorf("resolve predecessor: %w", err)
	}
	lock := stage.LockedPredecessor{ReportSHA256: decision.SHA256, Release: resolved.Release, IdentitySHA256: resolved.IdentitySHA256}
	if old := run.Snapshot.LockedPredecessor; old != nil && *old != lock {
		return nil, &releasechange.InputError{Reason: "resolved predecessor differs from locked decision"}
	}
	run.Snapshot.LockedPredecessor = &lock
	items, err := s.Plan(ctx, env, run)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(lock)
	if err != nil {
		return nil, fmt.Errorf("encode predecessor lock: %w", err)
	}
	if err := store.New(tx).SetRunPredecessorLock(ctx, store.SetRunPredecessorLockParams{ID: run.ID, LockedPredecessor: raw}); err != nil {
		return nil, fmt.Errorf("store predecessor lock: %w", err)
	}
	return items, nil
}
