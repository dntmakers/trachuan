package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

func (t *openAITrial) count(ctx context.Context, body, payload []byte) (int64, error) {
	key := openAICountKey(body)
	if n := t.counts[key]; n > 0 {
		return n, nil
	}
	if t.state.Phase != trialPreparing {
		return 0, fmt.Errorf("reserved trial is missing a cached OpenAI count")
	}
	id, err := newID()
	if err != nil {
		return 0, err
	}
	dispatch := openAICountDispatch{ID: id, Key: key, Project: t.state.Project, Fingerprint: t.state.Fingerprint, Model: t.state.Model, Version: t.state.PriceVersion}
	if err = t.writeJSON("count-"+id+".dispatch.json", dispatch); err != nil {
		return 0, err
	}
	call, cancel := context.WithTimeout(ctx, time.Minute)
	response, callErr := t.client.Count(call, payload)
	cancel()
	result := openAICountResult{ID: id, Key: key, Unknown: true}
	if callErr == nil && response.Status == 200 {
		fields, decodeErr := batchapi.DecodeEvidenceObject(response.Body, "object", "input_tokens", "error")
		var object string
		n, ok := batchapi.TokenInteger(fields["input_tokens"], 1)
		if decodeErr == nil && ok && json.Unmarshal(fields["object"], &object) == nil && object == "response.input_tokens" && (len(fields["error"]) == 0 || string(fields["error"]) == "null") {
			result.InputTokens, result.Unknown = n, false
		}
	}
	if err = t.writeJSON("count-"+id+".result.json", result); err != nil {
		return 0, err
	}
	if result.Unknown {
		return 0, fmt.Errorf("OpenAI trial count unavailable")
	}
	t.counts[key] = result.InputTokens
	if err = t.writeJSON("counts.json", t.counts); err != nil {
		return 0, err
	}
	return result.InputTokens, nil
}

func (t *openAITrial) loadCounts() error {
	t.counts = map[string]int64{}
	if _, err := readTrialJSON(t.path("counts.json"), &t.counts); err != nil {
		return err
	}
	entries, err := os.ReadDir(t.o.Out)
	if err != nil {
		return fmt.Errorf("read trial count journal: %w", err)
	}
	known := map[string]int64{}
	for _, entry := range entries {
		base, dispatch := strings.CutSuffix(entry.Name(), ".dispatch.json")
		id, count := strings.CutPrefix(base, "count-")
		if !dispatch || !count {
			continue
		}
		var d openAICountDispatch
		found, err := readTrialJSON(t.path(entry.Name()), &d)
		if err != nil || !found || !openAIDispatchValid(d) || d.ID != id || d.Model != t.state.Model || d.Version != t.state.PriceVersion || d.Project != t.state.Project || d.Fingerprint != t.state.Fingerprint {
			return fmt.Errorf("invalid OpenAI trial count dispatch")
		}
		var result openAICountResult
		found, err = readTrialJSON(t.path(base+".result.json"), &result)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if result.ID != id || result.Key != d.Key || result.InputTokens < 0 || result.Unknown != (result.InputTokens == 0) {
			return fmt.Errorf("invalid OpenAI trial count result")
		}
		if result.Unknown {
			continue
		}
		if prior := known[d.Key]; prior != 0 && prior != result.InputTokens {
			return fmt.Errorf("conflicting OpenAI trial counts")
		}
		known[d.Key] = result.InputTokens
	}
	for key, n := range t.counts {
		if n < 1 || known[key] != n {
			return fmt.Errorf("invalid trial count cache")
		}
	}
	if len(known) != len(t.counts) {
		t.counts = known
		return t.writeJSON("counts.json", known)
	}
	return nil
}
