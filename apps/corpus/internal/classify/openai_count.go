package classify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"danny.vn/trachuan/apps/corpus/internal/openaibatch"
	"golang.org/x/sys/unix"
)

type openAICountClient interface {
	Count(context.Context, []byte) (openaibatch.Response, error)
}
type openAICountRequest struct {
	Model, Version, Project, Fingerprint string
	BatchBody, CountBody                 []byte
}
type openAICountDispatch struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Project     string `json:"project"`
	Fingerprint string `json:"fingerprint"`
	Model       string `json:"model"`
	Version     string `json:"version"`
}
type openAICountResult struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	InputTokens int64  `json:"input_tokens"`
	Unknown     bool   `json:"unknown"`
}

func openAICountKey(body []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte("openai\x00/v1/responses\x00"))
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
func openAIProjectValid(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
func openAIDispatchValid(d openAICountDispatch) bool {
	_, ok := openAIPriceForModel(d.Model)
	return validID(d.ID) && validKey(d.Key) && validID(d.Fingerprint) && openAIProjectValid(d.Project) && ok && d.Version == openAIBatchPriceVersion
}
func (r *Runner) openAICountRequest(ctx context.Context, req openAICountRequest, client openAICountClient) (int64, error) {
	return r.openAICountRequestWithSync(ctx, req, client, unix.Fsync)
}
func (r *Runner) openAICountRequestWithSync(ctx context.Context, req openAICountRequest, client openAICountClient, syncDir func(int) error) (int64, error) {
	d := openAICountDispatch{ID: strings.Repeat("0", 32), Key: openAICountKey(req.BatchBody), Project: req.Project, Fingerprint: req.Fingerprint, Model: req.Model, Version: req.Version}
	if !openAIDispatchValid(d) || len(req.BatchBody) == 0 || len(req.CountBody) == 0 || client == nil {
		return 0, fmt.Errorf("invalid OpenAI count request")
	}
	var count int64
	err := r.countSnapshot(true, syncDir, func(s *openAICountStore, counts map[string]int64, pairs []openAICountPair) error {
		var e error
		count, e = countKnown(d.Key, counts, pairs)
		if e != nil || count > 0 {
			return e
		}
		d.ID, e = newID()
		if e != nil {
			return e
		}
		if e = s.write(s.journal, d.ID+".dispatch.json", d); e != nil {
			return e
		}
		return s.barrier()
	})
	if err != nil {
		return 0, fmt.Errorf("invalid OpenAI count evidence")
	}
	if count > 0 {
		return count, nil
	}
	response, callErr := client.Count(ctx, req.CountBody)
	result := openAICountResult{ID: d.ID, Key: d.Key, Unknown: true}
	if callErr == nil && response.Status == 200 {
		fields, e := batchapi.DecodeEvidenceObject(response.Body, "object", "input_tokens", "error")
		var object string
		if e == nil && json.Unmarshal(fields["object"], &object) == nil && object == "response.input_tokens" && (len(fields["error"]) == 0 || string(fields["error"]) == "null") {
			if n, ok := batchapi.TokenInteger(fields["input_tokens"], 1); ok {
				result.InputTokens = n
				result.Unknown = false
			}
		}
	}
	err = r.withOpenAICountStore(false, syncDir, func(s *openAICountStore) error {
		var e error
		if s.journal < 0 {
			return fmt.Errorf("missing OpenAI dispatch")
		}
		raw, e := countReadFile(s.journal, d.ID+".dispatch.json")
		if e != nil {
			return e
		}
		var retained openAICountDispatch
		if countDecode(raw, &retained, "id", "key", "project", "fingerprint", "model", "version") != nil || retained != d {
			return fmt.Errorf("invalid OpenAI dispatch")
		}
		if e = s.write(s.journal, d.ID+".result.json", result); e != nil {
			return e
		}
		if e = s.barrier(); e != nil {
			return e
		}
		if result.Unknown {
			return nil
		}
		counts, pairs, e := s.snapshot()
		if e != nil {
			return e
		}
		known, e := countKnown(d.Key, counts, pairs)
		if e != nil || known != result.InputTokens {
			return fmt.Errorf("conflicting OpenAI counts")
		}
		if counts[d.Key] != 0 {
			return nil
		}
		if e = s.write(s.counts, d.Key+".json", countedRequest{Key: d.Key, Count: result.InputTokens}); e != nil {
			return e
		}
		return s.barrier()
	})
	if err != nil {
		return 0, fmt.Errorf("persist OpenAI count result")
	}
	if result.Unknown {
		return 0, fmt.Errorf("OpenAI count unavailable")
	}
	return result.InputTokens, nil
}
