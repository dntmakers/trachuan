package classify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

type storedCapture struct {
	dir       string
	createdAt time.Time
	records   int
	proof     captureReference
}

func (store retainedStore) evidenceFor(s Submission, batchID string) (batchDownload, error) {
	if batchID != "" {
		return collectBatchEvidence(s, batchID, store.downloads[batchID], false)
	}
	ids := make([]string, 0, len(store.downloads))
	for id := range store.downloads {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var found batchDownload
	for _, id := range ids {
		download, err := collectBatchEvidence(s, id, store.downloads[id], true)
		if err != nil {
			return batchDownload{}, err
		}
		if len(download.results) == 0 {
			continue
		}
		if found.id != "" {
			return batchDownload{}, fmt.Errorf("multiple batches match submission %s in retained evidence: %s and %s", s.ID, found.id, id)
		}
		found = download
	}
	if found.id != "" {
		return store.evidenceFor(s, found.id)
	}
	return found, nil
}

func collectBatchEvidence(s Submission, id string, captures []storedCapture, discovering bool) (batchDownload, error) {
	download := batchDownload{id: id}
	kept := map[string]string{}
	for _, c := range captures {
		records := make([]capturedResult, c.records)
		claims := false
		for i := range records {
			if err := readRetainedFile(filepath.Join(c.dir, captureRecordName(i)), &records[i]); err != nil {
				return batchDownload{}, err
			}
			claims = claims || strings.HasPrefix(records[i].CustomID, s.ID+"-")
		}
		if discovering && !claims {
			continue
		}
		results := make([]batchapi.Result, 0, len(records))
		for i, record := range records {
			v, err := verifyCapturedLine(record)
			if err != nil {
				return batchDownload{}, fmt.Errorf("invalid captured result %s: %w", filepath.Join(c.dir, captureRecordName(i)), err)
			}
			results = append(results, v)
		}
		indexes, err := matchedDownloadIndexes(s, results)
		if err != nil {
			return batchDownload{}, fmt.Errorf("unverified capture %s: %w", c.dir, err)
		}
		if !c.createdAt.IsZero() {
			if !download.createdAt.IsZero() && !download.createdAt.Equal(c.createdAt) {
				return batchDownload{}, fmt.Errorf("conflicting batch creation time in capture %s", c.dir)
			}
			download.createdAt = c.createdAt
		}
		if c.proof.Path != "" && len(results) == len(indexes) {
			download.proofs = append(download.proofs, c.proof)
		}
		for _, v := range results {
			if prior, ok := kept[v.CustomID]; ok {
				if prior != v.RawJSON {
					return batchDownload{}, fmt.Errorf("conflicting evidence for item %s in capture %s", v.CustomID, c.dir)
				}
				continue
			}
			kept[v.CustomID] = v.RawJSON
			download.results = append(download.results, v)
		}
	}
	return download, nil
}

func verifyCapturedLine(v capturedResult) (batchapi.Result, error) {
	body, err := readObject([]byte(v.Body))
	if err != nil {
		return batchapi.Result{}, err
	}
	var kind string
	if err = json.Unmarshal(body["type"], &kind); err != nil || kind != v.Type {
		return batchapi.Result{}, fmt.Errorf("result type does not match captured body")
	}
	switch kind {
	case "succeeded", "expired", "canceled", "errored":
	default:
		return batchapi.Result{}, fmt.Errorf("unknown provider result type")
	}
	if v.Wire != "" {
		wire, decodeErr := readObject([]byte(v.Wire))
		if decodeErr != nil {
			return batchapi.Result{}, decodeErr
		}
		var id string
		if err = json.Unmarshal(wire["custom_id"], &id); err != nil || id != v.CustomID || !bytes.Equal(wire["result"], []byte(v.Body)) {
			return batchapi.Result{}, fmt.Errorf("record does not match the original provider line")
		}
	}
	return batchapi.Result{CustomID: v.CustomID, Type: kind, RawJSON: v.Body}, nil
}
