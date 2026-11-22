package classify

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type retainedStore struct {
	intents          []Submission
	byID             map[string]Submission
	receipts         map[string]batchRecord
	retryAbandoned   map[string]bool
	seenBatchID      map[string]string
	successfulCreate map[string]bool
	abandoned        map[string]bool
	abandonRecords   map[string]abandonmentRecord
	dispatched       map[string]bool
	claimed          map[string]bool
	downloads        map[string][]storedCapture
	attempts         map[string]map[int]retainedAttempt
	settlements      map[string]settlementRecord
	outcomes         map[string]map[string]Outcome
	verified         map[string]batchDownload
	missingRows      int
	pendingPilot     string
}

type retainedAttempt struct {
	record attemptRecord
	hash   string
}

func readRetainedDirectory(path string) ([]fs.DirEntry, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read classification evidence directory %s: %w", path, err)
	}
	return files, nil
}

func readRetainedFile(path string, value any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read classification record %s: %w", path, err)
	}
	if err = validateRetainedJSON(b, value); err != nil {
		return fmt.Errorf("decode classification record %s: %w", path, err)
	}
	if err = json.Unmarshal(b, value); err != nil {
		return fmt.Errorf("decode classification record %s: %w", path, err)
	}
	return nil
}

func (r *Runner) readStore() (retainedStore, error) {
	store := retainedStore{
		byID: map[string]Submission{}, receipts: map[string]batchRecord{}, retryAbandoned: map[string]bool{}, seenBatchID: map[string]string{}, successfulCreate: map[string]bool{},
		abandoned: map[string]bool{}, abandonRecords: map[string]abandonmentRecord{}, dispatched: map[string]bool{}, claimed: map[string]bool{}, downloads: map[string][]storedCapture{}, attempts: map[string]map[int]retainedAttempt{},
		settlements: map[string]settlementRecord{}, outcomes: map[string]map[string]Outcome{}, verified: map[string]batchDownload{},
	}
	root, err := readRetainedDirectory(r.IntentDir)
	if err != nil {
		return store, err
	}
	present := map[string]bool{}
	for _, f := range root {
		if !f.IsDir() || (f.Name() != "submissions" && f.Name() != "results" && f.Name() != "counts" && f.Name() != "evidence") {
			return store, fmt.Errorf("unrecognized intent store entry %s", filepath.Join(r.IntentDir, f.Name()))
		}
		present[f.Name()] = true
	}
	dir := filepath.Join(r.IntentDir, "submissions")
	var files []fs.DirEntry
	if present["submissions"] {
		files, err = readRetainedDirectory(dir)
		if err != nil {
			return store, err
		}
	}
	for _, f := range files {
		id, suffix, _ := strings.Cut(f.Name(), ".")
		path := filepath.Join(dir, f.Name())
		if !validID(id) || !f.Type().IsRegular() {
			return store, fmt.Errorf("invalid classification record path %s", path)
		}
		switch suffix {
		case "json":
			var s Submission
			if err = readRetainedFile(path, &s); err != nil {
				return store, err
			}
			if err = validateRecovery(s, id); err != nil {
				return store, fmt.Errorf("invalid intent %s: %w", path, err)
			}
			store.byID[id] = s
			store.intents = append(store.intents, s)
		case "batch":
			var record batchRecord
			if err = readRetainedFile(path, &record); err != nil {
				return store, err
			}
			store.receipts[id] = record
		case "settled":
			var record settlementRecord
			if err = readRetainedFile(path, &record); err != nil {
				return store, err
			}
			store.settlements[id] = record
		case "abandoned", "creator.lock", "creator.absent", "creator.finished", "creator.dispatch":
		default:
			return store, fmt.Errorf("unrecognized classification record %s", path)
		}
	}
	for id := range store.byID {
		store.retryAbandoned[id] = true
	}
	batchOwners := map[string]string{}
	for id, intent := range store.byID {
		responses, readErr := readProviderResponses(filepath.Join(r.IntentDir, "evidence", id, "responses"), id)
		if readErr != nil {
			return store, readErr
		}
		var saved *batchRecord
		if receipt, found := store.receipts[id]; found {
			saved = &receipt
		}
		receipt, found, seenID, successfulCreate, verifyErr := verifyReceipt(intent, saved, responses)
		if verifyErr != nil {
			return store, fmt.Errorf("verify provider receipt %s: %w", id, verifyErr)
		}
		if seenID != "" {
			store.seenBatchID[id] = seenID
		}
		if seenID != "" || successfulCreate {
			store.retryAbandoned[id] = false
		}
		store.successfulCreate[id] = successfulCreate
		if found {
			if ownerErr := registerBatchOwner(batchOwners, receipt.ID, id); ownerErr != nil {
				return store, ownerErr
			}
			store.receipts[id] = receipt
			store.retryAbandoned[id] = false
		}
		for _, response := range responses {
			if response.Operation != "create" || response.Status < 200 || response.Status >= 300 {
				continue
			}
			batch, identified, identityErr := observedBatchIdentity(response.Body)
			if identityErr != nil {
				return store, identityErr
			}
			if identified {
				if ownerErr := registerBatchOwner(batchOwners, batch, id); ownerErr != nil {
					return store, ownerErr
				}
			}
		}
	}
	for _, f := range files {
		id, suffix, _ := strings.Cut(f.Name(), ".")
		path := filepath.Join(dir, f.Name())
		s, ok := store.byID[id]
		if !ok {
			return store, fmt.Errorf("classification record %s has no matching intent %s", path, filepath.Join(dir, id+".json"))
		}
		switch suffix {
		case "batch":
			receipt := store.receipts[id]
			if !validReceipt(s, receipt) {
				return store, fmt.Errorf("invalid batch receipt %s for intent %s", path, filepath.Join(dir, id+".json"))
			}
			store.retryAbandoned[id] = false
		case "settled":
			record := store.settlements[id]
			if record.ID != id || record.BatchID == "" || record.BatchID != store.receipts[id].ID {
				return store, fmt.Errorf("settlement %s does not match batch receipt %s", path, filepath.Join(dir, id+".batch"))
			}
		case "abandoned":
			var record abandonmentRecord
			if err = readRetainedFile(path, &record); err != nil {
				return store, err
			}
			if !validAbandonment(s, record) {
				return store, fmt.Errorf("invalid abandonment record %s for intent %s", path, filepath.Join(dir, id+".json"))
			}
			store.abandoned[id] = true
			store.abandonRecords[id] = record
		case "creator.absent", "creator.finished", "creator.dispatch":
			var record creatorRecord
			if err = readRetainedFile(path, &record); err != nil {
				return store, err
			}
			if record.ID != id || record.ItemsHash != s.ItemsHash || record.At.Before(s.CreatedAt) || (suffix == "creator.dispatch" && !record.Dispatched) {
				return store, fmt.Errorf("invalid creator record %s for intent %s", path, filepath.Join(dir, id+".json"))
			}
			if record.Dispatched {
				store.dispatched[id] = true
				store.retryAbandoned[id] = false
			}
		}
	}
	results := filepath.Join(r.IntentDir, "results")
	var keys []fs.DirEntry
	if present["results"] {
		keys, err = readRetainedDirectory(results)
		if err != nil {
			return store, err
		}
	}
	for _, key := range keys {
		keyDir := filepath.Join(results, key.Name())
		if !key.IsDir() || !validKey(key.Name()) {
			return store, fmt.Errorf("invalid classification attempt directory %s", keyDir)
		}
		attempts, readErr := readRetainedDirectory(keyDir)
		if readErr != nil {
			return store, readErr
		}
		for _, f := range attempts {
			path := filepath.Join(keyDir, f.Name())
			id, index, ok := attemptFilename(f.Name())
			if !ok || !f.Type().IsRegular() {
				return store, fmt.Errorf("invalid classification attempt path %s", path)
			}
			s, known := store.byID[id]
			if !known || index >= len(s.Items) || s.Items[index].Local || s.Items[index].RequestHash != key.Name() || s.KeyVersion != keyVersion {
				return store, fmt.Errorf("attempt %s does not match intent %s", path, filepath.Join(dir, id+".json"))
			}
			s.BatchID = store.receipts[id].ID
			if s.BatchID == "" {
				return store, fmt.Errorf("attempt %s has no batch receipt %s", path, filepath.Join(dir, id+".batch"))
			}
			a, _, found, readErr := r.readAttempt(s, index)
			if readErr != nil {
				return store, fmt.Errorf("verify attempt %s: %w", path, readErr)
			}
			if !found {
				return store, fmt.Errorf("missing classification attempt %s", path)
			}
			if store.attempts[id] == nil {
				store.attempts[id] = map[int]retainedAttempt{}
			}
			hash := digest([]byte(a.Raw))
			a.Raw = ""
			store.attempts[id][index] = retainedAttempt{record: a, hash: hash}
			store.retryAbandoned[id] = false
		}
	}
	if present["evidence"] {
		if err = r.inspectCaptures(store.byID, &store); err != nil {
			return store, err
		}
	}
	if err = r.verifyAbandonments(&store); err != nil {
		return store, err
	}
	if present["counts"] {
		return store, r.inspectCounts()
	}
	return store, nil
}

func validReceipt(s Submission, receipt batchRecord) bool {
	return receipt.ID != "" && !receipt.SubmittedAt.Before(s.CreatedAt.Add(-time.Minute)) && receipt.DeadlineAt.Equal(receipt.SubmittedAt.Add(26*time.Hour))
}

func attemptFilename(name string) (string, int, bool) {
	stem := strings.TrimSuffix(name, ".json")
	id, idx, found := strings.Cut(stem, "-")
	if stem == name || !found || !validID(id) || len(idx) != 6 {
		return "", 0, false
	}
	for _, c := range idx {
		if c < '0' || c > '9' {
			return "", 0, false
		}
	}
	index, err := strconv.Atoi(idx)
	return id, index, err == nil
}
