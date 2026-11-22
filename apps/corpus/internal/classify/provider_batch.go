package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

type providerBatch struct {
	ID        string
	CreatedAt time.Time
	Status    string
}
type rawBatchClient interface {
	CreateRaw(context.Context, []batchapi.Request) ([]byte, int, error)
	GetRaw(context.Context, string) ([]byte, int, error)
}
type providerRequestEvidence struct {
	CustomID    string `json:"custom_id"`
	RequestHash string `json:"request_hash"`
}
type providerResponse struct {
	Owner              string                    `json:"intent_id"`
	ID                 string                    `json:"capture_id"`
	Operation          string                    `json:"operation"`
	BatchID            string                    `json:"provider_job_id,omitempty"`
	AccountFingerprint string                    `json:"account_fingerprint"`
	Status             int                       `json:"http_status"`
	Requests           []providerRequestEvidence `json:"requests,omitempty"`
	Body               []byte                    `json:"body"`
}

func (r *Runner) captureBatchResponse(record providerResponse) (providerResponse, error) {
	id, err := newID()
	if err != nil {
		return record, err
	}
	record.ID = id
	err = r.writeCapture(filepath.Join(r.IntentDir, "evidence", record.Owner, "responses", id+".json"), record)
	return record, err
}

func (r *Runner) providerCreate(ctx context.Context, owner string, requests []batchapi.Request) (providerBatch, error) {
	var intent Submission
	if err := readRetainedFile(filepath.Join(r.IntentDir, "submissions", owner+".json"), &intent); err != nil {
		return providerBatch{}, err
	}
	client, ok := r.Client.(rawBatchClient)
	identity, identified := r.Client.(interface{ AccountFingerprint() string })
	if !ok || !identified || !validID(intent.AccountFingerprint) || identity.AccountFingerprint() != intent.AccountFingerprint {
		return providerBatch{}, fmt.Errorf("provider response identity is unavailable")
	}
	refs := make([]providerRequestEvidence, 0, len(requests))
	for i := range requests {
		key, err := requestKey(&requests[i])
		if err != nil {
			return providerBatch{}, err
		}
		refs = append(refs, providerRequestEvidence{CustomID: requests[i].CustomID, RequestHash: key})
	}
	if err := verifyRequestScope(intent, refs); err != nil {
		return providerBatch{}, err
	}
	raw, status, callErr := client.CreateRaw(ctx, requests)
	if status == 0 && raw == nil {
		if callErr != nil {
			return providerBatch{}, callErr
		}
		return providerBatch{}, fmt.Errorf("provider create response is missing")
	}
	if raw == nil {
		raw = []byte{}
	}
	saved, err := r.captureBatchResponse(providerResponse{Owner: owner, Operation: "create", AccountFingerprint: intent.AccountFingerprint, Status: status, Requests: refs, Body: raw})
	if err != nil {
		return providerBatch{}, err
	}
	receipt, found, _, _, err := verifyReceipt(intent, nil, []providerResponse{saved})
	if err != nil {
		return providerBatch{}, err
	}
	if !found {
		if callErr != nil {
			return providerBatch{}, callErr
		}
		return providerBatch{}, fmt.Errorf("provider create response cannot establish receipt")
	}
	return providerBatch{ID: receipt.ID, CreatedAt: receipt.SubmittedAt}, callErr
}

func (r *Runner) providerGet(ctx context.Context, owner, batchID string) (providerBatch, error) {
	client, ok := r.Client.(rawBatchClient)
	identity, identified := r.Client.(interface{ AccountFingerprint() string })
	if !ok || !identified || !validID(identity.AccountFingerprint()) {
		return providerBatch{}, fmt.Errorf("provider response identity is unavailable")
	}
	raw, status, callErr := client.GetRaw(ctx, batchID)
	if status != 0 || raw != nil {
		if raw == nil {
			raw = []byte{}
		}
		if _, err := r.captureBatchResponse(providerResponse{Owner: owner, Operation: "get", BatchID: batchID, AccountFingerprint: identity.AccountFingerprint(), Status: status, Body: raw}); err != nil {
			return providerBatch{}, err
		}
	}
	if callErr != nil {
		return providerBatch{}, callErr
	}
	if status < 200 || status >= 300 {
		return providerBatch{}, fmt.Errorf("provider get response was not successful")
	}
	return decodeProviderBatch(raw, true, batchID)
}

func decodeProviderBatch(raw []byte, requireStatus bool, expected string) (providerBatch, error) {
	var batch providerBatch
	fields, err := batchapi.DecodeEvidenceObject(raw, "id", "created_at", "processing_status", "request_counts", "type", "error")
	if err != nil {
		return batch, fmt.Errorf("invalid provider batch response: %w", err)
	}
	if value := fields["error"]; len(value) > 0 && string(value) != "null" {
		return providerBatch{}, fmt.Errorf("provider response is an error envelope")
	}
	if value := fields["type"]; len(value) > 0 && string(value) != "null" {
		kind, kindErr := evidenceString(value)
		if kindErr != nil || kind != "message_batch" {
			return providerBatch{}, fmt.Errorf("invalid provider batch type")
		}
	}
	batch.ID, err = evidenceString(fields["id"])
	if err != nil || batch.ID == "" || (expected != "" && batch.ID != expected) {
		return providerBatch{}, fmt.Errorf("invalid provider batch identity")
	}
	if value := fields["created_at"]; len(value) > 0 && string(value) != "null" {
		if err = json.Unmarshal(value, &batch.CreatedAt); err != nil {
			return providerBatch{}, fmt.Errorf("invalid provider batch creation time")
		}
	}
	if value := fields["processing_status"]; len(value) > 0 && string(value) != "null" {
		if batch.Status, err = evidenceString(value); err != nil {
			return providerBatch{}, fmt.Errorf("invalid provider batch status")
		}
	}
	if requireStatus && batch.Status != "ended" && batch.Status != "in_progress" && batch.Status != "canceling" {
		return providerBatch{}, fmt.Errorf("invalid provider batch status")
	}
	if counts := fields["request_counts"]; counts != nil {
		if _, err = decodeListingCounts(counts); err != nil {
			return providerBatch{}, err
		}
	}
	return batch, nil
}

func readProviderResponses(dir, owner string) ([]providerResponse, error) {
	files, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read provider response captures: %w", err)
	}
	var records []providerResponse
	for _, file := range files {
		id, ok := strings.CutSuffix(file.Name(), ".json")
		if !ok || !validID(id) || !file.Type().IsRegular() {
			return nil, fmt.Errorf("invalid provider response capture path")
		}
		var response providerResponse
		if err = readRetainedFile(filepath.Join(dir, file.Name()), &response); err != nil {
			return nil, err
		}
		if response.Owner != owner || response.ID != id || (response.Operation != "create" && response.Operation != "get") || (response.Operation == "create" && response.BatchID != "") || (response.Operation == "get" && response.BatchID == "") || !validID(response.AccountFingerprint) || (response.Status != 0 && (response.Status < 100 || response.Status > 599)) || response.Body == nil {
			return nil, fmt.Errorf("invalid provider response capture")
		}
		if response.Operation == "get" && response.Status >= 200 && response.Status < 300 {
			observed, identified, identityErr := observedBatchIdentity(response.Body)
			if identityErr != nil {
				return nil, fmt.Errorf("invalid retained provider get identity: %w", identityErr)
			}
			if identified && observed != response.BatchID {
				return nil, fmt.Errorf("retained provider get batch identity differs")
			}
		}
		records = append(records, response)
	}
	return records, nil
}

func inspectProviderResponses(dir, owner string) error {
	_, err := readProviderResponses(dir, owner)
	return err
}
