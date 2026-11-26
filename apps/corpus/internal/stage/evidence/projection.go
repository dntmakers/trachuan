package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/build"
	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
)

func projectionReadError(err error) error {
	var invalid *corpusdb.InvalidProjectionError
	if errors.As(err, &invalid) {
		return refuse("read stored projection: %v", err)
	}
	return err
}

func (s *Stage) checkProjection(ctx context.Context, env stage.Env, run stage.Run, inputs frozenInputs, evidenceDir string) error {
	data, err := os.ReadFile(filepath.Join(inputs.buildDir, "build.json"))
	if err != nil {
		return refuse("read build record: %v", err)
	}
	var record build.BuildRecord
	if err = json.Unmarshal(data, &record); err != nil {
		return refuse("decode build record: %v", err)
	}
	if record.SchemaVersion < 9 {
		return nil
	}
	header, err := build.ProjectionHeader(run)
	if err != nil {
		return refuse("projection header: %v", err)
	}
	file, err := os.Open(filepath.Join(evidenceDir, changesFile))
	if err != nil {
		return refuse("open changes: %v", err)
	}
	defer func() { _ = file.Close() }()
	hasher, err := releasechanges.NewHasher(header)
	if err != nil {
		return refuse("projection header: %v", err)
	}
	if err = releasechange.ProjectStream(file, hasher.Add); err != nil {
		return refuse("project changes: %v", err)
	}
	sidecar := corpusdb.ProjectionResult{Header: header, Digest: hasher.Sum(), Counts: hasher.Counts()}
	if err = build.CompareProjection(record, header, sidecar); err != nil {
		return refuse("sidecar projection: %v", err)
	}
	pool, closePool, err := s.openRelease(ctx, env, run)
	if err != nil {
		return fmt.Errorf("open build for projection: %w", err)
	}
	defer closePool()
	stored, err := corpusdb.ReadReleaseChanges(ctx, pool, corpusdb.ReleaseChangeLimits(), nil)
	if err != nil {
		return projectionReadError(err)
	}
	if err = build.CompareProjection(record, header, stored); err != nil {
		return refuse("stored projection: %v", err)
	}
	return nil
}
