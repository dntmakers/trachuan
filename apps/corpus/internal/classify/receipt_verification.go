package classify

import (
	"fmt"
	"time"
)

func verifyRequestScope(intent Submission, requests []providerRequestEvidence) error {
	expected := 0
	for i, item := range intent.Items {
		if item.Local {
			continue
		}
		if expected >= len(requests) || requests[expected].CustomID != customID(intent.ID, i) || requests[expected].RequestHash != item.RequestHash {
			return fmt.Errorf("provider create request scope differs from intent")
		}
		expected++
	}
	if expected == 0 || expected != len(requests) {
		return fmt.Errorf("provider create request scope differs from intent")
	}
	return nil
}

func verifyReceipt(intent Submission, saved *batchRecord, responses []providerResponse) (batchRecord, bool, string, bool, error) {
	var receipt batchRecord
	found, ambiguousTime := false, false
	seenID := ""
	weakID := ""
	successfulCreate := false
	if saved != nil {
		if !validReceipt(intent, *saved) {
			return receipt, false, seenID, successfulCreate, fmt.Errorf("invalid retained batch receipt")
		}
		receipt, found, seenID = *saved, true, saved.ID
	}
	for _, response := range responses {
		if response.Operation != "create" {
			continue
		}
		if response.AccountFingerprint != intent.AccountFingerprint || !validID(intent.AccountFingerprint) {
			return receipt, false, seenID, successfulCreate, fmt.Errorf("provider create account differs from intent")
		}
		if err := verifyRequestScope(intent, response.Requests); err != nil {
			return receipt, false, seenID, successfulCreate, err
		}
		if response.Status < 200 || response.Status >= 300 {
			continue
		}
		successfulCreate = true
		observed, identified, identityErr := observedBatchIdentity(response.Body)
		if identityErr != nil {
			return receipt, false, seenID, successfulCreate, identityErr
		}
		if identified {
			if seenID != "" && seenID != observed {
				return receipt, false, seenID, successfulCreate, fmt.Errorf("conflicting provider batch identities")
			}
			seenID = observed
		}
		if !identified {
			if weakID == "" {
				weakID = observed
			}
			continue
		}
		batch, err := decodeProviderBatch(response.Body, true, "")
		if err != nil || batch.CreatedAt.IsZero() {
			continue
		}
		submitted := batch.CreatedAt.UTC().Truncate(time.Microsecond)
		candidate := batchRecord{ID: batch.ID, SubmittedAt: submitted, DeadlineAt: submitted.Add(26 * time.Hour)}
		if !validReceipt(intent, candidate) {
			continue
		}
		if saved != nil || ambiguousTime {
			continue
		}
		if found && !receipt.SubmittedAt.Equal(candidate.SubmittedAt) {
			receipt = batchRecord{}
			found = false
			ambiguousTime = true
			continue
		}
		receipt, found = candidate, true
	}
	if seenID == "" {
		seenID = weakID
	}
	return receipt, found, seenID, successfulCreate, nil
}

func registerBatchOwner(owners map[string]string, batch, owner string) error {
	if previous, exists := owners[batch]; exists && previous != owner {
		return fmt.Errorf("provider batch identity belongs to multiple submissions")
	}
	owners[batch] = owner
	return nil
}
