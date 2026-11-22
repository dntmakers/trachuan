package evidence

import (
	"context"
	"fmt"
	"sync"

	"danny.vn/trachuan/apps/corpus/internal/detector"
	"danny.vn/trachuan/apps/corpus/internal/stage"
)

type ocrLabelDetector struct{}
type treeDivergenceDetector struct{}

func (ocrLabelDetector) Spec() detector.Detector { return specOf(detector.OCRLabelItemOutsideLabels) }
func (treeDivergenceDetector) Spec() detector.Detector {
	return specOf(detector.TreeTranscriptionDivergence)
}

func (ocrLabelDetector) Detect(ctx context.Context, in Inputs) (Observation, error) {
	inputs, err := detectorInputValues(ctx, in)
	if err != nil {
		return Observation{}, err
	}
	return observeOCRLabelItems(inputs)
}

func (treeDivergenceDetector) Detect(ctx context.Context, in Inputs) (Observation, error) {
	inputs, err := detectorInputValues(ctx, in)
	if err != nil {
		return Observation{}, err
	}
	return observeTreeTranscription(inputs)
}

func needsDetectorInputs(id string) bool {
	return id == detector.OCRLabelItemOutsideLabels || id == detector.TreeTranscriptionDivergence
}

func detectorInputAuthority(snapshot stage.Snapshot) error {
	if snapshot.ExtractRecordContract != stage.ExtractRecordV1 {
		return &IncompleteError{Reason: "detector inputs require the v1 extract record contract"}
	}
	if _, err := stage.ExtractContract(snapshot, snapshot.Recipes["extract"]); err != nil {
		return &IncompleteError{Reason: fmt.Sprintf("invalid frozen extract declaration: %v", err)}
	}
	return nil
}

func withDetectorInputs(ctx context.Context, in Inputs) Inputs {
	var once sync.Once
	var values []detectorInput
	var loadErr error
	in.detectorInputs = func() ([]detectorInput, error) {
		once.Do(func() {
			if loadErr = detectorInputAuthority(in.Run.Snapshot); loadErr != nil {
				return
			}
			if in.detectorReader != nil {
				values, loadErr = in.detectorReader()
				return
			}
			if in.Release == nil {
				loadErr = &IncompleteError{Reason: "release database unavailable"}
				return
			}
			values, loadErr = loadDetectorInputs(ctx, in, sqlRelease{in.Release}, in.Run.Snapshot.Recipes["extract"])
		})
		return values, loadErr
	}
	return in
}

func detectorInputValues(ctx context.Context, in Inputs) ([]detectorInput, error) {
	if err := detectorInputAuthority(in.Run.Snapshot); err != nil {
		return nil, err
	}
	if in.detectorInputs == nil {
		in = withDetectorInputs(ctx, in)
	}
	return in.detectorInputs()
}
