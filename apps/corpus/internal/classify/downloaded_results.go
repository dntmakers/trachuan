package classify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

type batchDownload struct {
	id        string
	createdAt time.Time
	results   []batchapi.Result
	proofs    []captureReference
}

func (r *Runner) recordDownloaded(ctx context.Context, target Submission, expectedBatch string) (Submission, bool, error) {
	var applyErr error
	recorded := false
	result, err := r.transition(ctx, target.ID, func(latest *Submission) (bool, error) {
		changed := false
		rows, listErr := r.Repository.List(ctx)
		if listErr != nil {
			return false, listErr
		}
		_, readErr := r.verifyStore(rows, func(store retainedStore) error {
			receipt := store.receipts[latest.ID]
			if expectedBatch != "" && receipt.ID != expectedBatch {
				return fmt.Errorf("downloaded results do not match retained provider batch identity")
			}
			wasSettled := latest.Settled
			changed = latest.BatchID == "" && store.attachReceipt(latest)
			if store.restoreRecords(latest) || wasSettled {
				changed = changed || !wasSettled
				recorded = true
				return nil
			}
			download := store.verified[latest.ID]
			if receipt.ID == "" {
				var err error
				download, err = store.evidenceFor(*latest, "")
				if err != nil {
					return err
				}
			}
			if len(download.results) == 0 {
				if receipt.ID != "" {
					applyErr = errUnpricedAttempts
				}
				return nil
			}
			recorded = true
			if latest.BatchID == "" {
				if receipt.ID == "" {
					if !sameReconciliationTarget(target, *latest) {
						return fmt.Errorf("submission changed before provider batch attachment")
					}
					var err error
					receipt, err = r.writeDiscoveredReceipt(*latest, download)
					if err != nil {
						return err
					}
				}
				latest.BatchID = receipt.ID
				latest.SubmittedAt = &receipt.SubmittedAt
				latest.DeadlineAt = &receipt.DeadlineAt
				if latest.State == "uncertain" || latest.State == "preparing" {
					latest.State = "submitted"
				}
				changed = true
			}
			applyErr = r.applyMatchedDownload(latest, download)
			changed = changed || applyErr == nil || len(latest.attempts) != 0
			return nil
		})
		return changed, readErr
	})
	if err != nil {
		return result, recorded, err
	}
	return result, recorded, applyErr
}

func (r *Runner) writeDiscoveredReceipt(s Submission, download batchDownload) (batchRecord, error) {
	if !download.createdAt.IsZero() {
		s.SubmittedAt = &download.createdAt
	}
	if err := r.recordBatch(&s, download.id); err != nil {
		return batchRecord{}, err
	}
	return batchRecord{ID: s.BatchID, SubmittedAt: *s.SubmittedAt, DeadlineAt: *s.DeadlineAt}, nil
}

func (r *Runner) recordKnownDownload(ctx context.Context, batchID string) error {
	unlock, err := r.Repository.Lock(ctx)
	if err != nil {
		return err
	}
	rows, err := r.Repository.List(ctx)
	unlock()
	if err != nil {
		return err
	}
	var target *Submission
	for _, candidate := range rows {
		if candidate.BatchID == "" {
			if _, restoreErr := r.restoreReceipt(&candidate); restoreErr != nil {
				return restoreErr
			}
		}
		if candidate.BatchID != batchID {
			continue
		}
		if target != nil {
			return fmt.Errorf("multiple submissions own provider batch %s", batchID)
		}
		target = &candidate
	}
	if target == nil {
		return nil
	}
	_, _, err = r.recordDownloaded(ctx, *target, batchID)
	if errors.Is(err, errUnpricedAttempts) {
		return nil
	}
	return err
}

func (r *Runner) applyMatchedDownload(s *Submission, download batchDownload) error {
	if s.BatchID == "" || s.BatchID != download.id {
		return fmt.Errorf("downloaded results do not match submission provider batch identity")
	}
	if s.KeyVersion == keyVersion {
		byID, err := matchedDownloadIndexes(*s, download.results)
		if err != nil {
			return err
		}
		indexed := make(map[int]bool, len(s.attempts))
		for _, a := range s.attempts {
			indexed[a.Index] = true
		}
		for _, result := range download.results {
			i := byID[result.CustomID]
			a, _, parseErr := parseAttempt(*s, i, result.RawJSON)
			if parseErr != nil {
				return parseErr
			}
			if err = r.writeAttempt(*s, i, a); err != nil {
				return err
			}
			if !indexed[i] {
				a.Raw = ""
				s.attempts = append(s.attempts, a)
				indexed[i] = true
			}
		}
	}
	return r.settleDownload(s, download)
}
