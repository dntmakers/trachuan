package classify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (r *Runner) verifyAbandonments(store *retainedStore) error {
	for id, record := range store.abandonRecords {
		s := store.byID[id]
		if record.captureReference == (captureReference{}) {
			if store.dispatched[id] || len(store.attempts[id]) != 0 {
				return fmt.Errorf("abandonment %s has no listing evidence", id)
			}
			continue
		}
		if err := r.verifyAbandonmentListing(s, record); err != nil {
			return fmt.Errorf("invalid abandonment evidence for %s: %w", id, err)
		}
		if _, receipt := store.receipts[id]; !receipt && store.seenBatchID[id] == "" && !store.successfulCreate[id] && len(store.attempts[id]) == 0 && !store.claimed[id] {
			store.retryAbandoned[id] = true
		}
	}
	return nil
}

func (r *Runner) verifyAbandonmentListing(s Submission, record abandonmentRecord) error {
	parts := strings.Split(record.Path, "/")
	if len(parts) != 4 || parts[0] != "evidence" || parts[1] != s.ID || parts[2] != "list" || !validID(parts[3]) || !validKey(record.Hash) {
		return fmt.Errorf("invalid listing reference")
	}
	dir := filepath.Join(r.IntentDir, filepath.FromSlash(record.Path))
	files, err := readRetainedDirectory(dir)
	if err != nil {
		return err
	}
	proof, err := captureProof(r.IntentDir, dir, files)
	if err != nil {
		return err
	}
	if proof != record.captureReference {
		return fmt.Errorf("listing capture hash differs")
	}
	var start captureStart
	if err = readRetainedFile(filepath.Join(dir, "start.json"), &start); err != nil {
		return err
	}
	if !validID(s.AccountFingerprint) || start.AccountFingerprint != s.AccountFingerprint {
		return fmt.Errorf("listing account differs from intent")
	}
	if start.Owner != s.ID || start.ID != parts[3] || start.BatchID != "" || start.After == nil || start.Before == nil || start.CapturedAt == nil || start.After.After(s.CreatedAt.Add(-time.Minute)) || start.Before.Before(s.CreatedAt.Add(24*time.Hour)) || start.CapturedAt.Before(s.CreatedAt.Add(24*time.Hour)) || start.CapturedAt.After(record.AbandonedAt) || !start.Before.Equal(*start.CapturedAt) {
		return fmt.Errorf("listing does not cover the required window")
	}
	if err = r.verifyListingCreator(s, *start.CapturedAt); err != nil {
		return err
	}
	var end captureEnd
	if err = readRetainedFile(filepath.Join(dir, "end.json"), &end); err != nil {
		return err
	}
	if end.Error != "" || end.Records < 1 || len(files) != end.Records+2 {
		return fmt.Errorf("listing capture incomplete")
	}
	var pager listingPager
	for i := 0; i < end.Records; i++ {
		var raw string
		if err = readRetainedFile(filepath.Join(dir, captureRecordName(i)), &raw); err != nil {
			return err
		}
		page, pageErr := pager.accept(raw)
		if pageErr != nil {
			return pageErr
		}
		for _, candidate := range page.Data {
			if candidate.ID == "" || candidate.CreatedAt.IsZero() {
				return fmt.Errorf("invalid candidate identity")
			}
			if candidate.CreatedAt.Before(*start.After) || candidate.CreatedAt.After(*start.Before) {
				continue
			}
			if candidate.Status != "ended" {
				return fmt.Errorf("candidate batch is not ended")
			}
			total, countErr := candidate.Counts.total()
			if countErr != nil {
				return countErr
			}
			if err = r.verifyAbandonmentCandidate(s.ID, candidate.ID, s.AccountFingerprint, total); err != nil {
				return err
			}
		}
	}
	if !pager.complete() {
		return fmt.Errorf("listing has no final page")
	}
	return nil
}

func (r *Runner) verifyAbandonmentCandidate(owner, batchID, account string, total int64) error {
	scope := filepath.Join(r.IntentDir, "evidence", owner, captureScope(batchID))
	captures, err := os.ReadDir(scope)
	if err != nil {
		return fmt.Errorf("candidate results missing: %w", err)
	}
	complete := false
	for _, entry := range captures {
		dir := filepath.Join(scope, entry.Name())
		var start captureStart
		if err = readRetainedFile(filepath.Join(dir, "start.json"), &start); err != nil {
			return err
		}
		if start.Owner != owner || start.BatchID != batchID {
			return fmt.Errorf("candidate result identity differs")
		}
		if !validID(account) || start.AccountFingerprint != account {
			continue
		}
		files, readErr := readRetainedDirectory(dir)
		if readErr != nil {
			return readErr
		}
		var end captureEnd
		endErr := readRetainedFile(filepath.Join(dir, "end.json"), &end)
		clean := true
		ids := map[string]bool{}
		for _, f := range files {
			if f.Name() == "start.json" || f.Name() == "end.json" {
				continue
			}
			var item capturedResult
			if err = readRetainedFile(filepath.Join(dir, f.Name()), &item); err != nil {
				return err
			}
			line, wireErr := verifyCapturedLine(item)
			if item.Wire == "" || wireErr != nil || !validProviderCustomID(line.CustomID) || ids[line.CustomID] {
				clean = false
				continue
			}
			ids[line.CustomID] = true
			if strings.HasPrefix(item.CustomID, owner+"-") {
				clean = false
			}
		}
		if clean && endErr == nil && end.Error == "" && end.Records >= 0 && int64(end.Records) == total && len(ids) == end.Records && len(files) == end.Records+2 {
			complete = true
		}
	}
	if !complete {
		return fmt.Errorf("candidate results incomplete")
	}
	return nil
}
