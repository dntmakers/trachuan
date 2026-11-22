package classify

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"time"
)

func (r *Runner) verifyStore(rows []Submission, use func(retainedStore) error) (retainedStore, error) {
	var store retainedStore
	verify := func() error {
		if err := r.verifyLedgerRows(rows, &store); err != nil {
			return err
		}
		for _, intent := range store.intents {
			s := intent
			if store.attachReceipt(&s) {
				download, err := store.evidenceFor(s, s.BatchID)
				if err != nil {
					return err
				}
				if err = r.verifyCapturedAttempts(s, store, download); err != nil {
					return err
				}
				store.verified[s.ID] = download
			}
			if disk, ok := store.settlements[s.ID]; ok {
				outcomes, err := r.verifySettlement(s, disk, store)
				if err != nil {
					return fmt.Errorf("verify settlement %s: %w", r.settlementPath(s), err)
				}
				store.outcomes[s.ID] = outcomes
			}
		}
		if use != nil {
			return use(store)
		}
		return nil
	}
	err := r.withStoreLock(false, func() error {
		var err error
		store, err = r.readStore()
		if err != nil {
			return err
		}
		return verify()
	})
	if errors.Is(err, errStoreRootMissing) {
		err = verify()
	}
	return store, err
}

func (r *Runner) verifyCapturedAttempts(s Submission, store retainedStore, download batchDownload) error {
	if s.KeyVersion != keyVersion {
		return nil
	}
	byID, err := matchedDownloadIndexes(s, download.results)
	if err != nil {
		return err
	}
	_, required := store.settlements[s.ID]
	for _, v := range download.results {
		i := byID[v.CustomID]
		a, found := store.attempts[s.ID][i]
		if (!found && required) || (found && digest([]byte(v.RawJSON)) != a.hash) {
			return fmt.Errorf("attempt %s is missing or differs from its capture", r.attemptPath(s, i))
		}
	}
	return nil
}

func (r *Runner) verifyLedgerRows(rows []Submission, store *retainedStore) error {
	known := make(map[string]bool, len(rows))
	for _, row := range rows {
		known[row.ID] = true
		intent, found := store.byID[row.ID]
		path := filepath.Join(r.IntentDir, "submissions", row.ID+".json")
		if !found {
			return fmt.Errorf("classification ledger is missing retained intent %s", path)
		}
		for _, f := range []struct {
			name  string
			match bool
		}{
			{"provider", row.Provider == intent.Provider},
			{"project", row.ProjectID == intent.ProjectID},
			{"reserve", row.ReserveMicroUSD == intent.ReserveMicroUSD},
			{"budget name", row.Budget == intent.Budget},
			{"budget amount", row.BudgetMicroUSD == intent.BudgetMicroUSD},
			{"recipe hash", row.Recipe.Hash() == intent.Recipe.Hash()},
			{"family menu", slices.Equal(row.Families, intent.Families)},
			{"item list hash", row.ItemsHash == intent.ItemsHash && itemsHash(row.Items) == intent.ItemsHash},
			{"key version", row.KeyVersion == intent.KeyVersion},
			{"price version", row.PriceVersion == intent.PriceVersion},
			{"run id", row.RunID == intent.RunID},
			{"country and release", row.Country == intent.Country && row.Release == intent.Release},
			{"screen hash", row.ScreenHash == intent.ScreenHash},
			{"creation time", row.CreatedAt.Equal(intent.CreatedAt)},
		} {
			if !f.match {
				return fmt.Errorf("workshop %s differs from retained intent %s", f.name, path)
			}
		}
		receipt := store.receipts[row.ID]
		if !workshopReceiptMatches(row, receipt) {
			return fmt.Errorf("workshop batch receipt fields differ from %s", filepath.Join(r.IntentDir, "submissions", row.ID+".batch"))
		}
		if !row.Settled {
			if !nonterminal(row.State) && row.State != "failed" && row.State != "abandoned" {
				return fmt.Errorf("workshop settled flag conflicts with state %s for %s", row.State, path)
			}
			if row.CostMicroUSD != 0 {
				return fmt.Errorf("unsettled workshop cost must be zero for %s", path)
			}
			if row.State == "abandoned" && !store.abandoned[row.ID] {
				return fmt.Errorf("abandoned workshop row has no retained record %s", filepath.Join(r.IntentDir, "submissions", row.ID+".abandoned"))
			}
			continue
		}
		disk, exists := store.settlements[row.ID]
		settledPath := r.settlementPath(row)
		if !exists {
			if row.KeyVersion == 0 && row.BatchID != "" && validSettlementState(row.State) && row.CostMicroUSD >= 0 {
				store.pendingPilot = settledPath
				continue
			}
			return fmt.Errorf("settled ledger row has no retained settlement %s", settledPath)
		}
		if row.CostMicroUSD != disk.Cost {
			return fmt.Errorf("workshop cost differs from retained settlement %s", settledPath)
		}
		if row.State != disk.State || row.BatchID != disk.BatchID {
			return fmt.Errorf("workshop settled identity differs from retained settlement %s", settledPath)
		}
	}
	for _, intent := range store.intents {
		if !known[intent.ID] {
			store.missingRows++
		}
	}
	return nil
}

func workshopReceiptMatches(row Submission, receipt batchRecord) bool {
	if row.BatchID == "" {
		if row.SubmittedAt != nil || row.DeadlineAt != nil || row.Settled {
			return false
		}
		switch row.State {
		case "preparing", "uncertain", "abandoned", "failed":
			return true
		default:
			return false
		}
	}
	return row.BatchID == receipt.ID && sameReceiptTime(row.SubmittedAt, receipt.SubmittedAt) && sameReceiptTime(row.DeadlineAt, receipt.DeadlineAt)
}

func sameReceiptTime(got *time.Time, want time.Time) bool {
	return got != nil && got.Equal(want)
}

func (store retainedStore) ledgerCostFor(provider string) (int64, error) {
	if provider != "anthropic" && provider != "openai" {
		return 0, fmt.Errorf("invalid ledger provider")
	}
	if provider != "openai" && store.pendingPilot != "" {
		return 0, fmt.Errorf("pilot settlement %s must be imported with corpus submissions --rebuild before admission", store.pendingPilot)
	}
	var total int64
	for _, intent := range store.intents {
		p := intent.Provider
		if p == "" {
			if _, ok := priceForModel(intent.Recipe.Model); !ok || (intent.PriceVersion != "" && intent.PriceVersion != pilotPriceVersion && intent.PriceVersion != batchPriceVersion) {
				return 0, fmt.Errorf("unknown historical provider for %s", intent.ID)
			}
			p = "anthropic"
		}
		if p != "anthropic" && p != "openai" {
			return 0, fmt.Errorf("unknown provider for %s", intent.ID)
		}
		if p != provider {
			continue
		}
		charge := intent.ReserveMicroUSD
		if store.abandoned[intent.ID] && store.retryAbandoned[intent.ID] {
			charge = 0
		}
		if settled, found := store.settlements[intent.ID]; found {
			charge = settled.Cost
		}
		if charge < 0 || charge > math.MaxInt64-total {
			return 0, fmt.Errorf("invalid submission ledger cost for %s", intent.ID)
		}
		total += charge
	}
	return total, nil
}

func (store retainedStore) attachReceipt(s *Submission) bool {
	receipt, found := store.receipts[s.ID]
	if !found {
		return false
	}
	s.BatchID, s.SubmittedAt, s.DeadlineAt = receipt.ID, &receipt.SubmittedAt, &receipt.DeadlineAt
	if s.State == "preparing" || s.State == "uncertain" {
		s.State = "submitted"
	}
	return true
}

func (store retainedStore) restoreRecords(s *Submission) bool {
	s.attempts = nil
	for i := range s.Items {
		if a, found := store.attempts[s.ID][i]; found {
			s.attempts = append(s.attempts, a.record)
		}
	}
	v, found := store.settlements[s.ID]
	if found {
		s.State, s.CostMicroUSD, s.Settled, s.Results = v.State, v.Cost, true, store.outcomes[s.ID]
	} else if intent, ok := store.byID[s.ID]; ok && !s.Settled {
		s.Results = intent.Results
	}
	return found
}
