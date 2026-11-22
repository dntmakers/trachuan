package classify

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

type attemptRecord struct {
	SubmissionID string     `json:"submission_id"`
	BatchID      string     `json:"provider_job_id"`
	Index        int        `json:"item_index"`
	Key          string     `json:"key"`
	Outcome      string     `json:"outcome"`
	Raw          string     `json:"provider_body"`
	Usage        tokenUsage `json:"usage"`
	Cost         *int64     `json:"cost_micro_usd"`
	PriceVersion string     `json:"price_version"`
}

type providerResult struct {
	Type    string             `json:"type"`
	Message *anthropic.Message `json:"message,omitempty"`
	Error   json.RawMessage    `json:"error,omitempty"`
}

func rawResult(v batchapi.Result) (string, error) {
	if v.RawJSON != "" {
		return v.RawJSON, nil
	}
	b := providerResult{Type: v.Type, Error: json.RawMessage(v.ErrorJSON)}
	if v.Type == "succeeded" {
		if v.Message.RawJSON() != "" {
			return `{"type":"succeeded","message":` + v.Message.RawJSON() + `}`, nil
		}
		b.Message = &v.Message
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return "", fmt.Errorf("encode classification result: %w", err)
	}
	return string(raw), nil
}

func parseAttempt(s Submission, i int, raw string) (attemptRecord, anthropic.Message, error) {
	a := attemptRecord{SubmissionID: s.ID, BatchID: s.BatchID, Index: i, Key: s.Items[i].RequestHash, Outcome: "failed", Raw: raw, PriceVersion: s.priceVersion()}
	typeFields, err := batchapi.DecodeEvidenceProjection([]byte(raw), "type")
	if err != nil {
		return a, anthropic.Message{}, nil
	}
	var kind string
	if len(typeFields["type"]) == 0 || typeFields["type"][0] != '"' || json.Unmarshal(typeFields["type"], &kind) != nil {
		return a, anthropic.Message{}, nil
	}
	var msg anthropic.Message
	switch kind {
	case "expired", "canceled":
		a.Cost = new(int64)
	case "errored":
	case "succeeded":
		fields, bodyErr := providerEvidenceBody([]byte(raw))
		if bodyErr != nil || fields["message"] == nil {
			return a, msg, nil
		}
		usageFields, usageErr := batchapi.DecodeEvidenceProjection(fields["message"], "usage")
		if usageErr == nil {
			a.Usage, a.Cost = priceUsage(s, string(usageFields["usage"]))
		}
		var contentErr error
		msg, _, contentErr = decodeMessageEvidence(fields["message"])
		if contentErr != nil {
			return a, anthropic.Message{}, nil
		}
		if finalResponse(msg, s.Recipe.Article != nil) {
			a.Outcome = "final"
		}
	default:
		return a, msg, nil
	}
	return a, msg, nil
}

func (r *Runner) attemptPath(s Submission, i int) string {
	return filepath.Join(r.IntentDir, "results", s.Items[i].RequestHash, customID(s.ID, i)+".json")
}

func (r *Runner) writeAttempt(s Submission, i int, a attemptRecord) error {
	if !validID(s.ID) || i < 0 || i >= len(s.Items) || !validKey(a.Key) || r.IntentDir == "" {
		return fmt.Errorf("invalid classification attempt path")
	}
	existing, _, found, err := r.readAttempt(s, i)
	if err != nil {
		return err
	}
	if found {
		usageMatches := (existing.Cost == nil && a.Cost == nil) || existing.Usage == a.Usage
		if existing.SubmissionID != a.SubmissionID || existing.BatchID != a.BatchID || existing.Index != a.Index || existing.Key != a.Key || existing.Raw != a.Raw || existing.Outcome != a.Outcome || existing.PriceVersion != a.PriceVersion || !usageMatches || !equalCost(existing.Cost, a.Cost) {
			return fmt.Errorf("existing classification attempt differs")
		}
		return nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("encode classification attempt: %w", err)
	}
	return writeOnce(r.attemptPath(s, i), b)
}

func (r *Runner) readAttempt(s Submission, i int) (attemptRecord, anthropic.Message, bool, error) {
	var a attemptRecord
	var msg anthropic.Message
	if !validID(s.ID) || !validKey(s.Items[i].RequestHash) || r.IntentDir == "" {
		return a, msg, false, fmt.Errorf("invalid classification attempt path")
	}
	b, err := os.ReadFile(r.attemptPath(s, i))
	if errors.Is(err, os.ErrNotExist) {
		return a, msg, false, nil
	}
	if err != nil {
		return a, msg, false, fmt.Errorf("read classification attempt: %w", err)
	}
	if err = validateRetainedJSON(b, &a); err != nil {
		return a, msg, false, fmt.Errorf("decode classification attempt: %w", err)
	}
	if err = json.Unmarshal(b, &a); err != nil {
		return a, msg, false, fmt.Errorf("decode classification attempt: %w", err)
	}
	want, msg, err := parseAttempt(s, i, a.Raw)
	if err != nil {
		return a, msg, false, err
	}
	usageMatches := (a.Cost == nil && want.Cost == nil) || a.Usage == want.Usage
	if a.SubmissionID != want.SubmissionID || a.BatchID != want.BatchID || a.Index != want.Index || a.Key != want.Key || a.Outcome != want.Outcome ||
		a.PriceVersion != want.PriceVersion || !usageMatches || !equalCost(a.Cost, want.Cost) {
		return a, msg, false, fmt.Errorf("classification attempt does not match provider evidence")
	}
	return a, msg, true, nil
}

func equalCost(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
