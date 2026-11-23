package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

const openAITrialResultLimit = 200_000_000

func (t *openAITrial) collect(ctx context.Context, batch openAIBatchTerminal) error {
	capture := openAIBatchCapture{BatchID: batch.Identity.BatchID, OutputFileID: batch.OutputFileID, ErrorFileID: batch.ErrorFileID}
	out, outErr := t.download(ctx, batch.OutputFileID, "output")
	bad, badErr := t.download(ctx, batch.ErrorFileID, "error")
	capture.Output, capture.Error = out, bad
	if err := errors.Join(outErr, badErr); err != nil {
		cost := t.price(t.observedResults(capture), false, batch)
		for _, item := range cost.Items {
			if item.Status != "missing" && item.CostMicroUSD == nil {
				return errors.Join(err, t.finish(cost))
			}
		}
		return err
	}
	lines, err := replayOpenAITrialCoverage(t.parts, t.state.Stage, batch, capture)
	if err != nil {
		lines = t.observedResults(capture)
	}
	return t.finish(t.price(lines, err == nil && batch.Status != "cancelled", batch))
}

func replayOpenAITrialCoverage(parts []openAITrialItem, stage string, batch openAIBatchTerminal, capture openAIBatchCapture) ([]openAICoveredResult, error) {
	if isOpenAIBatchZeroWork(batch) {
		lines := make([]openAICoveredResult, 0, len(parts))
		for _, part := range parts {
			lines = append(lines, openAICoveredResult{CustomID: part.ID, Result: openAIResultLine{Status: openAIResultFailed, ZeroCost: true}})
		}
		return lines, nil
	}
	ids := make([]string, 0, len(parts))
	for _, part := range parts {
		ids = append(ids, part.ID)
	}
	return verifyOpenAIBatchCoverage(batch, capture, ids, stage)
}

type openAITrialFileBinding struct {
	BatchID string `json:"batch_id"`
	FileID  string `json:"file_id"`
	Hash    string `json:"sha256"`
	Capture string `json:"capture"`
}

func (b openAITrialFileBinding) valid(batch, part string) bool {
	return batch != "" && b.BatchID == batch && openAIUsableID(b.FileID) && validKey(b.Hash) && validTrialCaptureName(b.Capture, part)
}

func (t *openAITrial) download(ctx context.Context, id, part string) (*openAIFileCapture, error) {
	name := part + ".jsonl"
	raw, found, err := readTrialFile(t.path(name))
	if err != nil {
		return nil, err
	}
	binding := &t.state.Output
	if part == "error" {
		binding = &t.state.Error
	}
	if id == "" {
		if *binding != nil || found {
			return nil, fmt.Errorf("trial %s binding differs from terminal file", name)
		}
		return nil, nil
	}
	if *binding != nil {
		b := *binding
		if !b.valid(t.state.BatchID, part) || b.FileID != id {
			return nil, fmt.Errorf("trial %s binding differs from terminal file", name)
		}
		meta, captured, exists, captureErr := t.readCapture(b.Capture)
		if captureErr != nil || !exists || !meta.usable() || meta.Status != 200 || meta.FileID != id || meta.BatchID != b.BatchID || meta.Hash != b.Hash || (found && digest(raw) != b.Hash) {
			return nil, fmt.Errorf("trial %s capture differs from its binding", name)
		}
		raw = captured
	} else {
		b, recovered, recoveryErr := t.recoverResultCapture(id, part)
		if recoveryErr != nil {
			return nil, recoveryErr
		}
		if found && (b == nil || digest(raw) != b.Hash) {
			return nil, fmt.Errorf("unbound trial %s; existing file cannot be used", name)
		}
		if b == nil {
			if t.state.Phase == trialSettled {
				return nil, fmt.Errorf("missing settled trial %s binding", name)
			}
			b, recovered, err = t.captureFile(ctx, id, part)
			if err != nil {
				return nil, err
			}
		}
		raw, *binding = recovered, b
		if err = t.save(); err != nil {
			return nil, err
		}
	}
	if !found {
		if err = t.raw(name, raw); err != nil {
			return nil, err
		}
	}
	return trialFileCapture(raw), nil
}

func trialFileCapture(raw []byte) *openAIFileCapture {
	raw = bytes.TrimSuffix(raw, []byte{'\n'})
	capture := &openAIFileCapture{Complete: true}
	if len(raw) > 0 {
		capture.Lines = bytes.Split(raw, []byte{'\n'})
	}
	return capture
}

func (t *openAITrial) recoverResultCapture(id, part string) (*openAITrialFileBinding, []byte, error) {
	entries, err := os.ReadDir(t.o.Out)
	if err != nil {
		return nil, nil, fmt.Errorf("read trial result captures: %w", err)
	}
	var binding *openAITrialFileBinding
	var raw []byte
	for _, entry := range entries {
		name := entry.Name()
		if !validTrialCaptureName(name, part) {
			continue
		}
		meta, captured, exists, err := t.readCapture(name)
		if err != nil || !exists || meta.BatchID != t.state.BatchID || meta.FileID != id {
			return nil, nil, fmt.Errorf("trial %s capture differs from terminal identity", part)
		}
		if !meta.Complete || meta.Status != 200 {
			continue
		}
		if binding != nil {
			if binding.Hash != meta.Hash {
				return nil, nil, fmt.Errorf("conflicting complete trial %s captures", part)
			}
			continue
		}
		binding, raw = &openAITrialFileBinding{BatchID: meta.BatchID, FileID: id, Hash: meta.Hash, Capture: name}, captured
	}
	return binding, raw, nil
}

func (t *openAITrial) captureFile(ctx context.Context, id, part string) (*openAITrialFileBinding, []byte, error) {
	attempt, err := newID()
	if err != nil {
		return nil, nil, err
	}
	capture := part + "-" + attempt + ".capture"
	call, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	response, callErr := t.client.Content(call, id)
	raw := response.Body
	complete := callErr == nil && response.Stream != nil
	if response.Stream != nil {
		var readErr error
		raw, readErr = io.ReadAll(io.LimitReader(response.Stream, openAITrialResultLimit+1))
		closeErr := response.Stream.Close()
		complete = complete && readErr == nil && closeErr == nil && len(raw) <= openAITrialResultLimit
	}
	raw = raw[:min(len(raw), openAITrialResultLimit)]
	meta := openAITrialCapture{Status: response.Status, Complete: complete, BatchID: t.state.BatchID, FileID: id}
	if err = t.capture(capture, meta, raw); err != nil {
		return nil, nil, err
	}
	if !complete || response.Status != 200 {
		return nil, nil, fmt.Errorf("OpenAI trial %s download incomplete; rerun to resume", part)
	}
	return &openAITrialFileBinding{BatchID: meta.BatchID, FileID: id, Hash: digest(raw), Capture: capture}, raw, nil
}

func (t *openAITrial) observedResults(capture openAIBatchCapture) []openAICoveredResult {
	byID := make(map[string]openAIResultLine, len(t.parts))
	seen := make(map[string]bool, len(t.parts))
	for _, file := range []*openAIFileCapture{capture.Output, capture.Error} {
		if file == nil {
			continue
		}
		for _, raw := range file.Lines {
			fields, err := batchapi.DecodeEvidenceProjection(raw, "custom_id")
			if err != nil {
				continue
			}
			id, err := evidenceString(fields["custom_id"])
			i, indexErr := strconv.Atoi(id)
			if err != nil || indexErr != nil || i < 0 || i >= len(t.result.Items) || t.result.Items[i].Local || strconv.Itoa(i) != id {
				continue
			}
			if seen[id] {
				delete(byID, id)
				continue
			}
			seen[id] = true
			if result, parseErr := parseOpenAIResultLine(raw, id, t.state.Stage); parseErr == nil {
				byID[id] = result
			}
		}
	}
	lines := make([]openAICoveredResult, 0, len(byID))
	for id, line := range byID {
		lines = append(lines, openAICoveredResult{CustomID: id, Result: line})
	}
	return lines
}

func (t *openAITrial) apply(index int, line openAIResultLine) {
	if line.Status != openAIResultFinal {
		return
	}
	item := t.result.Items[index]
	if item.Article != nil {
		articles, dropped := ValidateArticles("end_turn", line.OutputText, *item.Article, t.nodes)
		t.result.Results[item.Key] = Outcome{Articles: articles, DroppedAnchors: dropped}
		return
	}
	t.result.Results[item.Key] = Validate("end_turn", line.OutputText, t.o.Recipe.F, t.result.Families)
}

func (t *openAITrial) finish(cost openAITrialCost) error {
	if err := t.retainUnsettledCost(cost); err != nil {
		return err
	}
	if cost.Complete && t.state.UnpricedResults > 0 {
		return fmt.Errorf("OpenAI trial has unreconciled unpriced results")
	}
	if t.state.Phase == trialSettled && (!cost.Complete || t.state.CostMicroUSD != cost.PricedMicroUSD) {
		return fmt.Errorf("settled trial evidence differs from retained cost")
	}
	if err := t.writeJSON("cost.json", cost); err != nil {
		return err
	}
	if !cost.Complete {
		return fmt.Errorf("OpenAI trial did not settle; see cost.json and retained batch files")
	}
	if t.state.Phase != trialSettled {
		t.state.Phase, t.state.CostMicroUSD = trialSettled, cost.PricedMicroUSD
		if err := t.save(); err != nil {
			return err
		}
	}
	result, err := json.Marshal(t.result)
	if err != nil {
		return fmt.Errorf("encode trial result: %w", err)
	}
	if err = t.client.guard(result); err != nil {
		return err
	}
	return writeFamilies(t.o.Out, t.result)
}
