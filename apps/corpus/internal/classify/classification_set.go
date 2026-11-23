package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
)

func ValidClassificationSetDigest(hash string) bool { return validKey(hash) }

func ReadClassificationSet(ctx context.Context, store artifact.Store, hash, country, taxonomy string) (ClassificationSet, error) {
	var set ClassificationSet
	if err := ctx.Err(); err != nil {
		return set, err
	}
	if store.Root == "" || !ValidClassificationSetDigest(hash) {
		return set, fmt.Errorf("classification set requires an artifact store and a SHA-256 digest")
	}
	root, err := openPackDir(store.Abs(ClassificationSetNamespace))
	if err != nil {
		return set, fmt.Errorf("open classification store: %w", err)
	}
	defer func() { _ = root.Close() }()
	dir, err := root.child(hash)
	if err != nil {
		return set, fmt.Errorf("open classification set: %w", err)
	}
	defer func() { _ = dir.Close() }()
	raw, err := dir.bound(ClassificationSetFilename, hash)
	if err != nil {
		return set, fmt.Errorf("verify classification set: %w", err)
	}
	if len(raw) > openAITrialResultLimit {
		return set, fmt.Errorf("classification set exceeds size limit")
	}
	if err = json.Unmarshal(raw, &set); err != nil {
		return set, fmt.Errorf("decode classification set: %w", err)
	}
	canonical, err := json.Marshal(set)
	if err != nil || !bytes.Equal(raw, canonical) {
		return set, fmt.Errorf("classification set is not in pack format")
	}
	if set.Version != 1 || set.Source.Country != country || taxonomy == "" || set.Taxonomy != taxonomy {
		return set, fmt.Errorf("classification set version, country or taxonomy mismatch")
	}
	policyHash, err := set.Policy.Hash()
	if err != nil || policyHash != set.PolicyHash {
		return set, fmt.Errorf("classification policy SHA-256 mismatch or invalid policy")
	}
	return set, ctx.Err()
}
