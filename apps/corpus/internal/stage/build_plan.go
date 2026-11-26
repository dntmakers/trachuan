package stage

import (
	"context"
	"errors"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/store"
	"github.com/jackc/pgx/v5"
)

type BuildReplacementError struct {
	State string
}

func (e *BuildReplacementError) Error() string {
	return fmt.Sprintf("cannot replace build item while run is %s or not at build stage", e.State)
}

func checkBuildReplacement(run store.Run) error {
	if run.State == "evaluated" || run.State == "released" || run.NextStage == nil || *run.NextStage != "build" {
		return &BuildReplacementError{State: run.State}
	}
	return nil
}

func prepareBuildItem(ctx context.Context, q *store.Queries, run store.Run, item Item) error {
	runID := run.ID
	params := store.GetPlannedItemInputHashParams{RunID: runID, Stage: "build", ItemKey: item.Key}
	old, err := q.GetPlannedItemInputHash(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read build item: %w", err)
	}
	if old != nil && *old == item.InputHash {
		return nil
	}
	if err = checkBuildReplacement(run); err != nil {
		return err
	}
	stages := []string{"build", "eval", EvidenceStage}
	if _, err = q.DeleteStageGaps(ctx, store.DeleteStageGapsParams{RunID: runID, Stages: stages}); err != nil {
		return fmt.Errorf("invalidate build gaps: %w", err)
	}
	if _, err = q.DeleteStageItems(ctx, store.DeleteStageItemsParams{RunID: runID, Stages: stages}); err != nil {
		return fmt.Errorf("invalidate build items: %w", err)
	}
	return nil
}

func verifyBuildItem(ctx context.Context, q *store.Queries, runID int64, item Item) error {
	stored, err := q.GetPlannedItemInputHash(ctx, store.GetPlannedItemInputHashParams{RunID: runID, Stage: "build", ItemKey: item.Key})
	if err != nil {
		return fmt.Errorf("read planned build item: %w", err)
	}
	if stored == nil || *stored != item.InputHash {
		return fmt.Errorf("planned build item input hash mismatch")
	}
	return nil
}
