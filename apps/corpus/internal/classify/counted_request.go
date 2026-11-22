package classify

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

type countedRequest struct {
	Key   string `json:"key"`
	Count int64  `json:"input_tokens"`
}

func (r *Runner) countPath(key string) string {
	return filepath.Join(r.IntentDir, "counts", key+".json")
}

func readCount(path, key string) (int64, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read token count %s: %w", path, err)
	}
	fields, err := batchapi.DecodeEvidenceObject(b, "key", "input_tokens")
	if err != nil {
		return 0, fmt.Errorf("decode token count %s: %w", path, err)
	}
	var storedKey string
	if len(fields["key"]) == 0 || fields["key"][0] != '"' || json.Unmarshal(fields["key"], &storedKey) != nil {
		return 0, fmt.Errorf("invalid token count %s", path)
	}
	count, ok := batchapi.TokenInteger(fields["input_tokens"], 1)
	if storedKey != key || !ok {
		return 0, fmt.Errorf("invalid token count %s", path)
	}
	return count, nil
}

func (r *Runner) writeCount(key string, n int64) error {
	if !validKey(key) || n <= 0 {
		return fmt.Errorf("invalid provider count for classification request key %s", key)
	}
	path := r.countPath(key)
	if err := r.writeCapture(path, countedRequest{Key: key, Count: n}); err != nil {
		return fmt.Errorf("record token count %s: %w", path, err)
	}
	return nil
}
