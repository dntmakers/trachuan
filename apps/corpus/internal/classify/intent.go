package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func writeOnce(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := durableDirectory(dir); err != nil {
		return fmt.Errorf("create submission directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".submission-")
	if err != nil {
		return fmt.Errorf("create submission file: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write submission file: %w", err)
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync submission file: %w", err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("close submission file: %w", err)
	}
	if err = os.Link(f.Name(), path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("publish submission file: %w", err)
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read existing submission file: %w", readErr)
		}
		if !bytes.Equal(b, data) {
			return fmt.Errorf("existing submission file differs")
		}
	}
	if err = os.Remove(f.Name()); err != nil {
		return fmt.Errorf("remove temporary submission file: %w", err)
	}
	return syncDirectory(dir)
}

func durableDirectory(dir string) error {
	err := os.Mkdir(dir, 0700)
	if errors.Is(err, os.ErrNotExist) {
		parent := filepath.Dir(dir)
		if parent == dir {
			return fmt.Errorf("create submission directory: %w", err)
		}
		if err = durableDirectory(parent); err != nil {
			return err
		}
		err = os.Mkdir(dir, 0700)
	}
	if err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create submission directory: %w", err)
	}
	if err == nil {
		return syncDirectory(filepath.Dir(dir))
	}
	return nil
}

func syncDirectory(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open submission directory: %w", err)
	}
	err = d.Sync()
	closeErr := d.Close()
	if err != nil {
		return fmt.Errorf("sync submission directory: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close submission directory: %w", closeErr)
	}
	return nil
}

func (r *Runner) writeIntent(s Submission) error {
	if r.IntentDir == "" {
		return fmt.Errorf("submission intent directory is required")
	}
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode submission intent: %w", err)
	}
	return writeOnce(filepath.Join(r.IntentDir, "submissions", s.ID+".json"), b)
}

type batchRecord struct {
	ID          string    `json:"provider_job_id"`
	SubmittedAt time.Time `json:"submitted_at"`
	DeadlineAt  time.Time `json:"deadline_at"`
}

func (r *Runner) writeBatch(s Submission) error {
	if s.SubmittedAt == nil || s.DeadlineAt == nil || s.BatchID == "" {
		return fmt.Errorf("missing provider batch identity")
	}
	record := batchRecord{ID: s.BatchID, SubmittedAt: s.SubmittedAt.UTC(), DeadlineAt: s.DeadlineAt.UTC()}
	if _, _, _, _, err := verifyReceipt(s, &record, nil); err != nil {
		return err
	}
	b, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode batch identity: %w", err)
	}
	return writeOnce(filepath.Join(r.IntentDir, "submissions", s.ID+".batch"), b)
}

func (r *Runner) attach(ctx context.Context, s *Submission, id string) error {
	if err := r.recordBatch(s, id); err != nil {
		return err
	}
	return r.Repository.Save(ctx, *s)
}

func (r *Runner) recordBatch(s *Submission, id string) error {
	now := s.CreatedAt
	if s.SubmittedAt != nil {
		now = s.SubmittedAt.UTC().Truncate(time.Microsecond)
	}
	deadline := now.Add(26 * time.Hour)
	s.BatchID = id
	s.State = "submitted"
	s.SubmittedAt = &now
	s.DeadlineAt = &deadline
	return r.writeBatch(*s)
}

func (r *Runner) retainedReceipt(s Submission) (batchRecord, bool, string, bool, error) {
	var empty batchRecord
	var intent Submission
	if err := readRetainedFile(filepath.Join(r.IntentDir, "submissions", s.ID+".json"), &intent); err != nil {
		return empty, false, "", false, err
	}
	if intent.ID != s.ID || intent.ItemsHash != s.ItemsHash || !intent.CreatedAt.Equal(s.CreatedAt) {
		return empty, false, "", false, fmt.Errorf("receipt intent differs from submission")
	}
	path := filepath.Join(r.IntentDir, "submissions", s.ID+".batch")
	var saved *batchRecord
	if _, err := os.Stat(path); err == nil {
		saved = new(batchRecord)
		if err = readRetainedFile(path, saved); err != nil {
			return empty, false, "", false, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return empty, false, "", false, fmt.Errorf("read provider batch identity: %w", err)
	}
	responses, err := readProviderResponses(filepath.Join(r.IntentDir, "evidence", s.ID, "responses"), s.ID)
	if err != nil {
		return empty, false, "", false, err
	}
	return verifyReceipt(intent, saved, responses)
}

func (r *Runner) restoreReceipt(s *Submission) (bool, error) {
	receipt, found, _, _, err := r.retainedReceipt(*s)
	if err != nil || !found {
		return found, err
	}
	s.BatchID, s.SubmittedAt, s.DeadlineAt = receipt.ID, &receipt.SubmittedAt, &receipt.DeadlineAt
	s.State = "submitted"
	return true, nil
}

func writeFamilies(dir string, s Submission) error {
	if s.Recipe.Article != nil {
		return writeArticles(dir, s)
	}
	artifact := struct {
		Version      int                `json:"version"`
		RecipeHash   string             `json:"recipe_hash"`
		TaxonomyHash string             `json:"taxonomy_hash"`
		Recipe       Recipe             `json:"recipe"`
		Documents    map[string]Outcome `json:"documents"`
		Selection    *DocumentSelection `json:"selection,omitempty"`
	}{1, s.Recipe.Hash(), s.Recipe.TaxonomyHash, s.Recipe, s.Results, nil}
	if len(s.Items) > 0 {
		artifact.Selection = s.Items[0].Selection
	}
	b, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return fmt.Errorf("encode family artifact: %w", err)
	}
	b = append(b, '\n')
	if err = writeOnce(filepath.Join(dir, "families.json"), b); err != nil {
		return err
	}
	return writeOnce(filepath.Join(dir, "MANIFEST.sha256"), []byte(digest(b)+"  families.json\n"))
}
