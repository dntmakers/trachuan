package releasecheck

import (
	"context"
	"errors"

	"danny.vn/trachuan/internal/corpusdb"
	"github.com/jackc/pgx/v5"
)

type ProjectionExpected struct {
	Version  int
	SHA256   string
	Release  string
	Previous string
}

type ProjectionReport struct {
	Version  int            `json:"version"`
	SHA256   string         `json:"sha256"`
	Release  string         `json:"release"`
	Previous *string        `json:"previous"`
	Baseline bool           `json:"baseline"`
	Counts   map[string]int `json:"counts"`
}

type projectionDB interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func checkProjection(ctx context.Context, db projectionDB, schema int, expected *ProjectionExpected) (*ProjectionReport, error) {
	if schema < 9 {
		if expected != nil {
			return nil, errors.New("projection arguments forbidden")
		}
		return nil, nil
	}
	if expected == nil {
		return nil, errors.New("projection arguments required")
	}
	result, err := corpusdb.ReadReleaseChanges(ctx, db, corpusdb.ReleaseChangeLimits(), nil)
	if err != nil {
		return nil, errors.New("invalid projection")
	}
	if result.Header.Version != expected.Version {
		return nil, errors.New("projection version mismatch")
	}
	if result.Header.Country != "vn" || result.Header.Release != expected.Release {
		return nil, errors.New("projection release mismatch")
	}
	if result.Header.Baseline != (result.Header.PredecessorIdentityHash == nil) {
		return nil, errors.New("invalid projection")
	}
	if expected.Previous == "" {
		if result.Header.Predecessor != nil {
			return nil, errors.New("projection previous release mismatch")
		}
	} else if result.Header.Predecessor == nil || *result.Header.Predecessor != expected.Previous {
		return nil, errors.New("projection previous release mismatch")
	}
	if result.Digest != expected.SHA256 {
		return nil, errors.New("projection digest mismatch")
	}
	return &ProjectionReport{Version: result.Header.Version, SHA256: result.Digest, Release: result.Header.Release, Previous: result.Header.Predecessor, Baseline: result.Header.Baseline, Counts: result.Counts}, nil
}
