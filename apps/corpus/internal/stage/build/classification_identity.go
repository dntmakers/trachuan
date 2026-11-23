package build

import (
	"context"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func (s *Stage) frozenInputHash(run stage.Run, inv Inventory, correction correctionInput) string {
	return withClassificationSet(s.inputHash(run.Country, run.Rel, inv, correction), run.Snapshot.ClassificationSetSHA256)
}

func withClassificationSet(input, selection string) string {
	if selection == "" {
		return input
	}
	return stage.RecipeHash("build-classification-set", "1", input, selection)
}

func readBuildClassification(ctx context.Context, store artifact.Store, run stage.Run, taxonomyVersion string) (*classify.ClassificationSet, error) {
	if run.Snapshot.ClassificationSetSHA256 == "" {
		return nil, nil
	}
	set, err := classify.ReadClassificationSet(ctx, store, run.Snapshot.ClassificationSetSHA256, run.Country, taxonomyVersion)
	if err != nil {
		return nil, err
	}
	return &set, nil
}
