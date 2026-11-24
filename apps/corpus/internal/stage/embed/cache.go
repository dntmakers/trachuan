package embed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/chunk"
	"danny.vn/trachuan/apps/corpus/internal/store"
)

type cachedRow struct {
	bytes []byte
	count int
}

type cachedArtifact struct {
	index   Index
	vectors []byte
	err     error
	stale   bool
}

type cacheReader interface {
	LookupEmbedRows(context.Context, store.LookupEmbedRowsParams) ([]store.LookupEmbedRowsRow, error)
}

func (s *Stage) lookup(ctx context.Context, env stage.Env, run stage.Run, item stage.Item, file chunk.File) (map[string]cachedRow, error) {
	return s.lookupFrom(ctx, store.New(env.DB), env, run, item, file)
}

func (s *Stage) lookupFrom(ctx context.Context, q cacheReader, env stage.Env, run stage.Run, item stage.Item, file chunk.File) (map[string]cachedRow, error) {
	hashes := make([]string, 0, len(file.Chunks))
	for _, c := range file.Chunks {
		hashes = append(hashes, InputSHA256(c.ContextPrefix, c.Content))
	}
	recipe := s.Recipe(run.Snapshot)
	rows, err := q.LookupEmbedRows(ctx, store.LookupEmbedRowsParams{RecipeHash: recipe, InputHashes: hashes})
	if err != nil {
		return nil, fmt.Errorf("lookup embed rows: %w", err)
	}
	hits := make(map[string]cachedRow)
	visitCachedRows(rows, func(row store.LookupEmbedRowsRow) cachedArtifact {
		return s.openCached(env.Artifacts, recipe, row)
	}, func(row store.LookupEmbedRowsRow, a cachedArtifact, reason error) {
		if reason != nil {
			slog.Warn("embed cache miss", "run", run.Country+"/"+run.Rel, "key", item.Key, "reason", reason)
			return
		}
		hits[row.InputSha256] = copyCachedRow(a, row)
	})
	return hits, nil
}

func visitCachedRows(rows []store.LookupEmbedRowsRow, open func(store.LookupEmbedRowsRow) cachedArtifact, visit func(store.LookupEmbedRowsRow, cachedArtifact, error)) {
	groups := make(map[int64][]store.LookupEmbedRowsRow)
	for _, row := range rows {
		groups[row.ArtifactID] = append(groups[row.ArtifactID], row)
	}
	for _, refs := range groups {
		a := open(refs[0])
		for _, r := range refs {
			visit(r, a, a.rowError(r))
		}
	}
}

func (a cachedArtifact) rowError(r store.LookupEmbedRowsRow) error {
	if a.err != nil {
		return a.err
	}
	if r.Row < 0 || int(r.Row) >= len(a.index.Rows) {
		return fmt.Errorf("row out of range")
	}
	row := a.index.Rows[r.Row]
	if row.InputSHA256 != r.InputSha256 || row.TokenCount != int(r.TokenCount) {
		return fmt.Errorf("index row mismatch")
	}
	return nil
}

func copyCachedRow(a cachedArtifact, row store.LookupEmbedRowsRow) cachedRow {
	width := a.index.Dimensions * 4
	start := int(row.Row) * width
	data := make([]byte, width)
	copy(data, a.vectors[start:start+width])
	return cachedRow{bytes: data, count: int(row.TokenCount)}
}

func (s *Stage) openCached(artifacts artifact.Store, recipe string, row store.LookupEmbedRowsRow) cachedArtifact {
	if row.ArtifactStage != "embed" || row.ArtifactRecipeHash != recipe {
		return cachedArtifact{stale: true, err: fmt.Errorf("artifact record stage or recipe mismatch")}
	}
	return s.openRecorded(artifacts, recipe, artifact.Result{InputHash: row.InputHash, Path: row.Path, SHA256: row.Sha256})
}

func (s *Stage) openRecorded(artifacts artifact.Store, recipe string, record artifact.Result) cachedArtifact {
	art, ok, err := artifacts.OpenRecorded("embed", record.InputHash, recipe, record.SHA256)
	if err != nil {
		return cachedArtifact{err: err}
	}
	if !ok {
		return cachedArtifact{stale: true, err: fmt.Errorf("artifact missing or verification failed")}
	}
	dir, err := filepath.Abs(artifacts.Abs(art.Path))
	if err != nil {
		return cachedArtifact{err: fmt.Errorf("resolve verified artifact path: %w", err)}
	}
	recordedPath, err := filepath.Abs(artifacts.Abs(record.Path))
	if err != nil {
		return cachedArtifact{err: fmt.Errorf("resolve recorded artifact path: %w", err)}
	}
	if recordedPath != dir {
		return cachedArtifact{stale: true, err: fmt.Errorf("artifact path mismatch")}
	}
	idx, err := s.readIndex(dir, record.InputHash)
	if err != nil {
		return invalidCachedArtifact(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "vectors.f32"))
	if err != nil {
		return invalidCachedArtifact(fmt.Errorf("read vectors: %w", err))
	}
	if err = validateVectors(data, len(idx.Rows), idx.Dimensions); err != nil {
		return invalidCachedArtifact(err)
	}
	return cachedArtifact{index: idx, vectors: data}
}

func (s *Stage) readIndex(dir, inputHash string) (Index, error) {
	idx, err := readVerifiedIndex(dir)
	if err != nil {
		return Index{}, err
	}
	if idx.Recipe != s.expected || idx.ChunkSHA256 != inputHash {
		return Index{}, fmt.Errorf("embed artifact identity mismatch")
	}
	return idx, nil
}

func invalidCachedArtifact(err error) cachedArtifact {
	var pathErr *os.PathError
	stale := !errors.As(err, &pathErr) || errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
	return cachedArtifact{err: err, stale: stale}
}
