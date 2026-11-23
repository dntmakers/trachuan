package classify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"golang.org/x/sys/unix"
)

func (t *openAITrial) withTrialBudget(summary *OpenAITrialSummary, apply func() error) error {
	if t.o.CapMicroUSD < 1 || t.o.CapMicroUSD > OpenAITrialMaxMicroUSD {
		return fmt.Errorf("invalid OpenAI trial cap (maximum 40 USD)")
	}
	parent := filepath.Dir(t.o.Out)
	unlock, err := trialLock(parent)
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := os.ReadDir(parent)
	if err != nil {
		return fmt.Errorf("read sibling trial directories: %w", err)
	}
	self, err := os.Stat(t.o.Out)
	if err != nil {
		return fmt.Errorf("inspect current trial directory: %w", err)
	}
	summary.SiblingMicroUSD = 0
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == filepath.Base(t.o.Out) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect sibling trial directory: %w", err)
		}
		if os.SameFile(self, info) {
			continue
		}
		charge, err := trialSiblingCharge(filepath.Join(parent, entry.Name()))
		summary.SiblingMicroUSD += min(charge, OpenAITrialMaxMicroUSD+1-summary.SiblingMicroUSD)
		if err != nil {
			return fmt.Errorf("OpenAI trial admission blocked by sibling directory %q: %w", entry.Name(), err)
		}
	}
	if summary.ReserveMicroUSD > t.o.CapMicroUSD-summary.SiblingMicroUSD {
		return fmt.Errorf("OpenAI trial exceeds --cap-usd (%d trial + %d siblings micro-USD)", summary.ReserveMicroUSD, summary.SiblingMicroUSD)
	}
	if apply != nil {
		return apply()
	}
	return nil
}

func trialSiblingCharge(dir string) (int64, error) {
	var state openAITrialState
	found, err := readTrialState(filepath.Join(dir, "state.json"), &state)
	if err != nil {
		return 0, err
	}
	if !found {
		traces, err := trialTraces(dir, true)
		if err != nil {
			return 0, fmt.Errorf("inspect trial traces: %w", err)
		}
		if traces {
			return 0, fmt.Errorf("trial traces lack valid state.json")
		}
		return 0, nil
	}
	switch state.Phase {
	case trialPreparing, trialReserved, trialUploading, trialUploaded, trialDispatched, trialSubmitted:
		if state.CostMicroUSD != 0 {
			return state.ReserveMicroUSD, fmt.Errorf("nonterminal state already carries recorded costs")
		}
		return state.ReserveMicroUSD, verifyTrialPendingEvidence(dir, state)
	case trialSettled:
		if err := verifyTrialSettledCost(dir, state); err != nil {
			return max(state.ReserveMicroUSD, state.CostMicroUSD), err
		}
		return state.CostMicroUSD, nil
	case trialTerminal:
		return max(state.ReserveMicroUSD, state.CostMicroUSD), fmt.Errorf("terminal batch is unsettled (%d unpriced results recorded); reconcile retained evidence before creating another batch", state.UnpricedResults)
	default:
		return 0, fmt.Errorf("unsupported trial phase")
	}
}

func verifyTrialPendingEvidence(dir string, state openAITrialState) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("inspect pending trial evidence: %w", err)
	}
	dispatched := state.Phase == trialDispatched || state.Phase == trialSubmitted
	for _, entry := range entries {
		name := entry.Name()
		create := name == "create.capture"
		retrieve := validTrialCaptureName(name, "retrieve")
		switch {
		case name == "terminal.json" || name == "cost.json" || name == "output.jsonl" || name == "error.jsonl" || validTrialCaptureName(name, "output") || validTrialCaptureName(name, "error"):
			return fmt.Errorf("pending state carries terminal trial evidence")
		case !dispatched && (create || retrieve || name == "create.json"):
			return fmt.Errorf("trial state lacks a marker for recorded dispatch evidence")
		case create || retrieve:
			meta, raw, exists, err := readTrialCapture(filepath.Join(dir, name))
			if err != nil || !exists || (retrieve && (meta.BatchID == "" || meta.BatchID != state.BatchID)) {
				return fmt.Errorf("cannot verify pending batch capture %q", name)
			}
			if !meta.usable() {
				continue
			}
			if _, terminal, err := trialBatchStatus(raw); err != nil || terminal {
				return fmt.Errorf("batch capture %q is terminal or has unknown status; reconcile retained evidence", name)
			}
		}
	}
	return nil
}

func verifyTrialSettledCost(dir string, state openAITrialState) error {
	invalid := func() error {
		return fmt.Errorf("settled trial lacks complete cost.json with explicit cost and zero unpriced results")
	}
	if state.UnpricedResults != 0 || state.CostMicroUSD > state.ReserveMicroUSD {
		return invalid()
	}
	raw, found, err := readTrialFile(filepath.Join(dir, "cost.json"))
	if err != nil || !found {
		return invalid()
	}
	fields, err := batchapi.DecodeEvidenceProjection(raw, "priced_micro_usd", "counted_input_tokens", "priced_input_tokens", "priced_output_tokens")
	if err != nil || fields["priced_micro_usd"] == nil || fields["counted_input_tokens"] == nil || fields["priced_input_tokens"] == nil || fields["priced_output_tokens"] == nil {
		return invalid()
	}
	var cost openAITrialCost
	if validateRetainedJSON(raw, &cost) != nil || json.Unmarshal(raw, &cost) != nil || !cost.Complete || cost.Issue != "" || cost.Unpriced == nil || len(cost.Unpriced) != 0 || cost.Items == nil || (state.ReserveMicroUSD == 0) != (len(cost.Items) == 0) || cost.PricedMicroUSD != state.CostMicroUSD {
		return invalid()
	}
	seen := make(map[string]bool, len(cost.Items))
	var amount, counted int64
	for _, item := range cost.Items {
		if item.CustomID == "" || seen[item.CustomID] || (item.Status != string(openAIResultFinal) && item.Status != string(openAIResultFailed)) || item.CostMicroUSD == nil || *item.CostMicroUSD < 0 || *item.CostMicroUSD > cost.PricedMicroUSD-amount || item.CountedInputTokens < 1 || item.CountedInputTokens > cost.CountedInputTokens-counted {
			return invalid()
		}
		seen[item.CustomID] = true
		amount += *item.CostMicroUSD
		counted += item.CountedInputTokens
	}
	if amount != cost.PricedMicroUSD || counted != cost.CountedInputTokens || cost.PricedInputTokens < 0 || cost.PricedOutputTokens < 0 {
		return invalid()
	}
	return nil
}

func (t *openAITrial) retainUnsettledCost(cost openAITrialCost) error {
	unpriced := max(t.state.UnpricedResults, len(cost.Unpriced))
	priced := max(t.state.CostMicroUSD, cost.PricedMicroUSD)
	if cost.Complete || (unpriced == t.state.UnpricedResults && priced == t.state.CostMicroUSD) {
		return nil
	}
	unlock, err := lockTrialDirectory(filepath.Dir(t.o.Out), unix.LOCK_EX)
	if err != nil {
		return err
	}
	defer unlock()
	t.state.UnpricedResults, t.state.CostMicroUSD = unpriced, priced
	return t.save()
}
