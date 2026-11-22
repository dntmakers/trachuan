package classify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

var errUnpricedAttempts = errors.New("classification attempts have incomplete or unpriced evidence; reservation retained")

type captureReference struct {
	Path string `json:"capture_path,omitempty"`
	Hash string `json:"capture_sha256,omitempty"`
}

type settlementRecord struct {
	ID           string             `json:"id"`
	BatchID      string             `json:"provider_job_id"`
	State        string             `json:"state"`
	Cost         int64              `json:"cost_micro_usd"`
	PriceVersion string             `json:"price_version"`
	Evidence     string             `json:"evidence_kind"`
	PilotResults map[string]Outcome `json:"pilot_results,omitempty"`
	captureReference
}

func validSettlementState(state string) bool {
	return state == "completed" || state == "cancelled" || state == "abandoned"
}

func (r *Runner) settlementPath(s Submission) string {
	return filepath.Join(r.IntentDir, "submissions", s.ID+".settled")
}

func (r *Runner) writeSettlement(s Submission, record settlementRecord) error {
	b, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode classification settlement: %w", err)
	}
	return writeOnce(r.settlementPath(s), b)
}

func (r *Runner) verifySettlement(s Submission, record settlementRecord, store retainedStore) (map[string]Outcome, error) {
	if record.ID != s.ID || s.BatchID == "" || record.BatchID != s.BatchID || record.PriceVersion != s.priceVersion() || record.Cost < 0 || !validSettlementState(record.State) {
		return nil, fmt.Errorf("invalid settlement identity, state or cost")
	}
	if s.KeyVersion == 0 && record.Evidence == "pilot_ledger" && record.captureReference == (captureReference{}) {
		return record.PilotResults, nil
	}
	if record.Evidence != "provider_results" || record.Path == "" || !validKey(record.Hash) || len(record.PilotResults) != 0 {
		return nil, fmt.Errorf("settlement has no valid complete capture reference")
	}
	download := store.verified[s.ID]
	matched := false
	for _, proof := range download.proofs {
		matched = matched || proof == record.captureReference
	}
	if !matched {
		return nil, fmt.Errorf("settlement capture %s is missing, incomplete, or differs from its hash", filepath.Join(r.IntentDir, record.Path))
	}
	cost, outcomes, err := priceCompleteDownload(s, download)
	if err != nil {
		return nil, err
	}
	if cost != record.Cost {
		return nil, fmt.Errorf("settlement cost differs from priced capture")
	}
	return outcomes, nil
}

func (r *Runner) settleDownload(s *Submission, download batchDownload) error {
	if len(download.proofs) == 0 {
		return errUnpricedAttempts
	}
	cost, outcomes, err := priceCompleteDownload(*s, download)
	if err != nil {
		return err
	}
	state := resultState(s.State)
	record := settlementRecord{ID: s.ID, BatchID: s.BatchID, State: state, Cost: cost, PriceVersion: s.priceVersion(), Evidence: "provider_results", captureReference: download.proofs[0]}
	if err = r.writeSettlement(*s, record); err != nil {
		return err
	}
	s.State, s.CostMicroUSD, s.Settled, s.Results = state, cost, true, outcomes
	return nil
}

func priceCompleteDownload(s Submission, download batchDownload) (int64, map[string]Outcome, error) {
	byID, err := matchedDownloadIndexes(s, download.results)
	if err != nil {
		return 0, nil, err
	}
	if len(download.proofs) == 0 || len(byID) == 0 || len(byID) != len(download.results) {
		return 0, nil, errUnpricedAttempts
	}
	if s.KeyVersion == 0 {
		return legacyOutcomes(s, byID, download.results)
	}
	outcomes := make(map[string]Outcome, len(s.Items))
	for _, item := range s.Items {
		if item.Local {
			outcomes[item.Key] = s.Results[item.Key]
		}
	}
	var cost int64
	for _, v := range download.results {
		i := byID[v.CustomID]
		a, msg, err := parseAttempt(s, i, v.RawJSON)
		if err != nil {
			return 0, nil, err
		}
		if a.Cost == nil || *a.Cost > math.MaxInt64-cost {
			return 0, nil, errUnpricedAttempts
		}
		cost += *a.Cost
		outcomes[s.Items[i].Key] = resultOutcome(s, s.Items[i], msg, a.Outcome == "final")
	}
	return cost, outcomes, nil
}

func resultState(state string) string {
	switch state {
	case "cancelling", "cancelled":
		return "cancelled"
	case "abandoned":
		return "abandoned"
	default:
		return "completed"
	}
}

func (r *Runner) recoverRetained(s *Submission, store retainedStore) (bool, error) {
	wasSettled := s.Settled
	attached := s.BatchID == "" && store.attachReceipt(s)
	if store.restoreRecords(s) {
		return attached || !wasSettled, nil
	}
	if wasSettled {
		return false, nil
	}
	if s.BatchID == "" {
		return false, nil
	}
	download := store.verified[s.ID]
	if len(download.results) == 0 {
		return attached || len(s.attempts) != 0, nil
	}
	err := r.applyMatchedDownload(s, download)
	if errors.Is(err, errUnpricedAttempts) {
		return attached || len(s.attempts) != 0, nil
	}
	return err == nil, err
}

func (r *Runner) recoverLedgerSettlements(ctx context.Context, rows []Submission, retained retainedStore) error {
	for i := range rows {
		found, err := r.recoverRetained(&rows[i], retained)
		if err != nil {
			return err
		}
		if found {
			if err = r.Repository.Save(ctx, rows[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func noResult(s Submission, item Item) Outcome {
	return resultOutcome(s, item, anthropic.Message{}, false)
}

func legacyOutcomes(s Submission, byID map[string]int, results []batchapi.Result) (int64, map[string]Outcome, error) {
	if _, ok := priceForModel(s.Recipe.Model); !ok {
		return 0, nil, fmt.Errorf("unknown classification model")
	}
	outcomes := make(map[string]Outcome, len(results))
	var costTicks int64
	price, _ := priceForModel(s.Recipe.Model)
	for _, v := range results {
		i := byID[v.CustomID]
		raw, err := rawResult(v)
		if err != nil {
			return 0, nil, err
		}
		a, msg, err := parseAttempt(s, i, raw)
		if err != nil {
			return 0, nil, err
		}
		if a.Cost == nil || a.Usage.Input > 100000 {
			return 0, nil, fmt.Errorf("classification attempt has no priced evidence; reservation retained")
		}
		costTicks += a.Usage.Input*price.input + a.Usage.Output*price.output
		outcomes[s.Items[i].Key] = resultOutcome(s, s.Items[i], msg, v.Type == "succeeded")
	}
	return (costTicks + priceUnit - 1) / priceUnit, outcomes, nil
}

func matchedDownloadIndexes(s Submission, results []batchapi.Result) (map[string]int, error) {
	byID := make(map[string]int, len(s.Items))
	for i, item := range s.Items {
		if !item.Local {
			byID[customID(s.ID, i)] = i
		}
	}
	seen := make(map[string]bool, len(results))
	for _, result := range results {
		if _, known := byID[result.CustomID]; !known || seen[result.CustomID] {
			return nil, fmt.Errorf("classification batch results have duplicate or foreign identities; reservation retained")
		}
		seen[result.CustomID] = true
	}
	return byID, nil
}
