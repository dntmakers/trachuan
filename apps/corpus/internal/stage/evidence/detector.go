package evidence

import (
	"context"

	"danny.vn/trachuan/apps/corpus/internal/detector"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Subject = releasechange.Subject

type Fired struct {
	Subject           Subject
	Discriminator     string
	Severity          detector.Severity
	FingerprintInputs []string
}

type Observation struct {
	Findings           []Fired
	Observed           int
	NotObservable      int
	Excluded           int
	ObservedCompletely func(Subject) bool
}

type Inputs struct {
	Run         stage.Run
	BuildDir    string
	BuildSHA256 string
	Build       func() (dir, sha256 string, err error)
	ReportPath  string
	Release     *pgxpool.Pool

	ArtifactDir func(stage, itemKey string) (string, bool, error)

	detectorInputs func() ([]detectorInput, error)
	detectorReader func() ([]detectorInput, error)
}

type Detector interface {
	Spec() detector.Detector
	Detect(context.Context, Inputs) (Observation, error)
}

type IncompleteError struct {
	Detector string
	Reason   string
}

func (e *IncompleteError) Error() string {
	if e.Detector == "" {
		return "detector_incomplete: " + e.Reason
	}
	return "detector_incomplete: " + e.Detector + ": " + e.Reason
}
