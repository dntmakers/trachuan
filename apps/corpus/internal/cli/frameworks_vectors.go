package cli

import (
	"context"
	"fmt"

	lawembed "danny.vn/trachuan/apps/corpus/internal/stage/embed"
	"danny.vn/trachuan/internal/frameworks/build"
)

func (s *frameworkEmbedder) Read(ctx context.Context, artifact build.EmbedArtifact, key string, chunks []build.Chunk, hash string) ([]build.Vector, error) {
	if err := s.Validate(ctx, artifact, key, chunks, hash); err != nil {
		return nil, err
	}
	idx, err := lawembed.DecodeIndex(artifact.Index)
	if err != nil {
		return nil, fmt.Errorf("read embedding index: %w", &frameworkEmbedError{cause: err})
	}

	rows := make([]build.Vector, 0, len(idx.Rows))
	for _, row := range idx.Rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		values, err := lawembed.ReadVectorBytes(artifact.Vectors, idx, row.Row)
		if err != nil {
			return nil, fmt.Errorf("read embedding vector: %w", &frameworkEmbedError{cause: err})
		}
		rows = append(rows, build.Vector{Ordinal: row.ChunkOrdinal, TokenCount: row.TokenCount, InputSHA256: row.InputSHA256, Values: values})
	}

	return rows, nil
}
