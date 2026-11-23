package run

import (
	"context"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func freezeClassificationSet(ctx context.Context, d Deps, country string, snapshot *stage.Snapshot, existing bool) error {
	selection := d.ClassificationSetSHA256
	if existing {
		if selection != "" && selection != snapshot.ClassificationSetSHA256 {
			return fmt.Errorf("cannot replace the frozen classification set; start a new release")
		}
		selection = snapshot.ClassificationSetSHA256
	}
	if selection == "" {
		return nil
	}
	if !containsStage(snapshot.Stages, "build") {
		return fmt.Errorf("classification set selection requires the build stage in the frozen run")
	}
	version := d.TaxonomyVersion
	if existing {
		version = snapshot.TaxonomyVersion
	}
	if _, err := classify.ReadClassificationSet(ctx, d.Artifacts, selection, country, version); err != nil {
		return err
	}
	snapshot.ClassificationSetSHA256 = selection
	snapshot.TaxonomyVersion = version
	return nil
}
