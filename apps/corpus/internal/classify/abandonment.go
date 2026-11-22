package classify

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type abandonmentRecord struct {
	ID          string    `json:"id"`
	ItemsHash   string    `json:"items_hash"`
	AbandonedAt time.Time `json:"abandoned_at"`
	captureReference
}

func (r *Runner) writeAbandonment(s *Submission, proof captureReference) error {
	known, err := r.restoreAbandonment(s)
	if err != nil || known {
		return err
	}
	b, err := json.Marshal(abandonmentRecord{ID: s.ID, ItemsHash: s.ItemsHash, AbandonedAt: r.Now().UTC().Truncate(time.Microsecond), captureReference: proof})
	if err != nil {
		return fmt.Errorf("encode submission abandonment: %w", err)
	}
	return writeOnce(filepath.Join(r.IntentDir, "submissions", s.ID+".abandoned"), b)
}

func (r *Runner) restoreAbandonment(s *Submission) (bool, error) {
	b, err := os.ReadFile(filepath.Join(r.IntentDir, "submissions", s.ID+".abandoned"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read submission abandonment: %w", err)
	}
	var record abandonmentRecord
	if err = validateRetainedJSON(b, &record); err != nil {
		return false, fmt.Errorf("decode submission abandonment: %w", err)
	}
	if err = json.Unmarshal(b, &record); err != nil {
		return false, fmt.Errorf("decode submission abandonment: %w", err)
	}
	if !validAbandonment(*s, record) {
		return false, fmt.Errorf("invalid submission abandonment")
	}
	s.State = "abandoned"
	return true, nil
}

func validAbandonment(s Submission, record abandonmentRecord) bool {
	return record.ID == s.ID && record.ItemsHash == s.ItemsHash && !record.AbandonedAt.Before(s.CreatedAt.Add(24*time.Hour))
}
