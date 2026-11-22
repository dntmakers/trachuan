package extract

import (
	"context"
	"fmt"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/store"
)

func (s *Stage) VerifyReuse(ctx context.Context, env stage.Env, run stage.Run, item stage.Item, art artifact.Result) error {
	if err := validateExtractContract(run.Snapshot.ExtractRecordContract); err != nil {
		return err
	}
	if run.Snapshot.ExtractRecordContract != stage.ExtractRecordV1 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	input, err := store.New(env.DB).GetExtractInput(ctx, store.GetExtractInputParams{RunID: run.ID, ItemKey: item.Key})
	if err != nil {
		return fmt.Errorf("load fetch artifact: %w", err)
	}
	members, err := verifiedFetchMembers(env.Artifacts.Abs(input.Path), input.Sha256)
	if err != nil {
		return err
	}
	selected, _, data, err := s.resolveFrozenV1(ctx, env, run, item, input, members)
	if err != nil {
		return err
	}
	detector, err := deriveDetectorInputsV1(selected, data)
	if err != nil {
		return fmt.Errorf("derive detector inputs: %w", err)
	}
	return admitExistingV1(env, art, input.Sha256, item.InputHash, selected.source, detector)
}
