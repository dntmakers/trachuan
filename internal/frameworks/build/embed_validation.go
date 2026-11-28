package build

import (
	"context"
	"fmt"
	"os"
)

func validateEmbeddings(ctx context.Context, base, dir string, service Embedder, hash string, staged bool, snapshots ...*snapshotParseInputs) error {
	frozen, err := snapshotForStep(base, snapshots...)
	if err != nil {
		return err
	}
	reg := frozen.registry
	chunks, err := readStepRows[Chunk](frozen, base, "chunk", "chunks.jsonl")
	if err != nil {
		return err
	}
	groups := map[string][]Chunk{}
	for _, version := range releaseVersions(reg) {
		for _, prefix := range []string{"fw:", "fwp:"} {
			groups[prefix+version.Code+":"+version.Version] = nil
		}
	}
	for _, row := range chunks {
		if _, ok := groups[row.DocumentKey]; !ok {
			return fmt.Errorf("unknown document key")
		}
		groups[row.DocumentKey] = append(groups[row.DocumentKey], row)
	}
	count, err := embeddingCount(base, dir, frozen.artifactState(), staged)
	if err != nil {
		if len(groups) == 0 && os.IsNotExist(err) {
			return nil
		}
		return hide("read embedding artifacts", err)
	}
	if count != len(groups) {
		return fmt.Errorf("embedding artifact count mismatch")
	}
	for key, rows := range groups {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("validate embeddings: %w", ctxErr)
		}
		var artifact EmbedArtifact
		if staged {
			artifact, err = stagedEmbedding(dir, key)
		} else {
			artifact, err = frozen.artifactState().embedding(base, key)
		}
		if err != nil {
			return err
		}
		if err := service.Validate(ctx, artifact, key, rows, hash); err != nil {
			return fmt.Errorf("invalid embedding artifact %s: %w", key, err)
		}
	}
	return nil
}
