package classify

import "fmt"

func validateRecovery(s Submission, id string) error {
	if s.ID != id || s.ItemsHash != itemsHash(s.Items) || s.Recipe.validate(s.KeyVersion == 0) != nil || s.ReserveMicroUSD < 0 || len(s.Items) == 0 || len(s.Items) > 100000 {
		return fmt.Errorf("invalid submission intent identity or ordered item hash")
	}
	legacy := s.KeyVersion == 0 && s.PriceVersion == ""
	current := s.KeyVersion == keyVersion && s.PriceVersion == batchPriceVersion
	if !legacy && !current {
		return fmt.Errorf("invalid classification intent versions")
	}
	if amount := BudgetMicroUSD(s.Budget); amount == 0 || s.BudgetMicroUSD != amount {
		return fmt.Errorf("invalid classification intent budget pair")
	}
	keys := make(map[string]bool, len(s.Items))
	var paid int
	var reserve int64
	for _, item := range s.Items {
		if s.KeyVersion == keyVersion {
			if !validKey(item.RequestHash) || keys[item.RequestHash] || item.Key == "" {
				return fmt.Errorf("invalid or duplicate classification intent key")
			}
			keys[item.RequestHash] = true
		}
		if item.Local {
			if _, exists := s.Results[item.Key]; !exists || s.KeyVersion != keyVersion {
				return fmt.Errorf("invalid local classification intent item")
			}
			continue
		}
		cost, err := s.Recipe.reserve(s.PriceVersion, item.InputTokens)
		if err != nil {
			return fmt.Errorf("validate recovered reservation: %w", err)
		}
		paid++
		reserve += cost
	}
	if paid == 0 || reserve != s.ReserveMicroUSD {
		return fmt.Errorf("recovered submission reservation differs from counted input and output bounds")
	}
	return nil
}

func (r *Runner) rebuildSubmission(s *Submission, prior Submission, retained retainedStore) error {
	s.State, s.BatchID, s.CostMicroUSD, s.Settled = "uncertain", "", 0, false
	s.SubmittedAt, s.DeadlineAt = nil, nil
	if s.KeyVersion == 0 {
		s.Results = map[string]Outcome{}
	}
	attached := retained.attachReceipt(s)
	if retained.abandoned[s.ID] {
		s.State = "abandoned"
	} else if attached && prior.BatchID == s.BatchID && (prior.State == "cancelling" || prior.State == "failed") {
		s.State = prior.State
	}
	if prior.Settled && s.KeyVersion == 0 {
		if _, found := retained.settlements[s.ID]; !found {
			record := settlementRecord{ID: s.ID, BatchID: s.BatchID, State: prior.State, Cost: prior.CostMicroUSD, PriceVersion: s.priceVersion(), Evidence: "pilot_ledger", PilotResults: prior.Results}
			if err := r.writeSettlement(*s, record); err != nil {
				return err
			}
			s.State, s.CostMicroUSD, s.Settled, s.Results = record.State, record.Cost, true, record.PilotResults
			return nil
		}
	}
	_, err := r.recoverRetained(s, retained)
	return err
}
