package classify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

type captureStart struct {
	Owner              string     `json:"intent_id"`
	ID                 string     `json:"capture_id"`
	BatchID            string     `json:"provider_job_id,omitempty"`
	After              *time.Time `json:"after,omitempty"`
	Before             *time.Time `json:"before,omitempty"`
	CreatedAt          *time.Time `json:"provider_created_at,omitempty"`
	CapturedAt         *time.Time `json:"captured_at,omitempty"`
	AccountFingerprint string     `json:"account_fingerprint,omitempty"`
}

type captureEnd struct {
	Records int    `json:"records"`
	Error   string `json:"download_error,omitempty"`
}

type capturedResult struct {
	CustomID string `json:"custom_id"`
	Type     string `json:"type"`
	Body     string `json:"result"`
	Wire     string `json:"provider_line,omitempty"`
}

type evidenceCapture struct {
	start captureStart
	dir   string
}

func captureScope(batchID string) string {
	if batchID == "" {
		return "list"
	}
	return digest([]byte(batchID))
}

func (r *Runner) writeCapture(path string, record any) error {
	b, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode classification provider evidence: %w", err)
	}
	return r.withStoreLock(true, func() error { return writeOnce(path, b) })
}

func (r *Runner) startCapture(start captureStart) (evidenceCapture, error) {
	if !validID(start.Owner) {
		return evidenceCapture{}, fmt.Errorf("provider evidence requires a submission intent")
	}
	id, err := newID()
	if err != nil {
		return evidenceCapture{}, err
	}
	start.ID = id
	dir := filepath.Join(r.IntentDir, "evidence", start.Owner, captureScope(start.BatchID), id)
	c := evidenceCapture{start: start, dir: dir}
	return c, r.writeCapture(filepath.Join(dir, "start.json"), start)
}

func captureRecordName(index int) string { return fmt.Sprintf("%08d.json", index) }

func (r *Runner) endCapture(c evidenceCapture, n int, cause error) error {
	end := captureEnd{Records: n}
	var reported error
	if cause != nil {
		if errors.Is(cause, io.ErrUnexpectedEOF) {
			reported = fmt.Errorf("classification provider download ended before EOF: %w", io.ErrUnexpectedEOF)
		} else {
			reported = providerError(cause)
		}
		end.Error = reported.Error()
	}
	return errors.Join(reported, r.writeCapture(filepath.Join(c.dir, "end.json"), end))
}

type captureWriteError struct{ err error }

func (e *captureWriteError) Error() string { return e.err.Error() }
func (e *captureWriteError) Unwrap() error { return e.err }

func (r *Runner) captureResults(ctx context.Context, owner, batchID string, createdAt time.Time) error {
	if batchID == "" {
		return fmt.Errorf("result stream requires a provider batch identity")
	}
	start := captureStart{Owner: owner, BatchID: batchID}
	if identity, ok := r.Client.(interface{ AccountFingerprint() string }); ok && validID(identity.AccountFingerprint()) {
		start.AccountFingerprint = identity.AccountFingerprint()
	}
	if !createdAt.IsZero() {
		at := createdAt.UTC().Truncate(time.Microsecond)
		start.CreatedAt = &at
	}
	c, err := r.startCapture(start)
	if err != nil {
		return err
	}
	n := 0
	err = r.Client.StreamResults(ctx, batchID, func(v batchapi.Result) error {
		body, bodyErr := rawResult(v)
		if v.WireJSON != "" {
			fields, wireErr := batchapi.DecodeEvidenceObject([]byte(v.WireJSON), "custom_id", "result")
			if wireErr == nil && fields["result"] != nil {
				body, bodyErr = string(fields["result"]), nil
			}
		}
		if bodyErr != nil && v.WireJSON == "" {
			return &captureWriteError{bodyErr}
		}
		record := capturedResult{CustomID: v.CustomID, Type: v.Type, Body: body, Wire: v.WireJSON}
		if writeErr := r.writeCapture(filepath.Join(c.dir, captureRecordName(n)), record); writeErr != nil {
			return &captureWriteError{writeErr}
		}
		n++
		return nil
	})
	return r.endCapture(c, n, err)
}

func (r *Runner) captureListing(ctx context.Context, lister batchLister, owner string, after time.Time) ([]listingBatch, captureReference, error) {
	at := r.Now().UTC().Truncate(time.Microsecond)
	before := at
	identity, ok := r.Client.(interface{ AccountFingerprint() string })
	if !ok || !validID(identity.AccountFingerprint()) {
		return nil, captureReference{}, fmt.Errorf("classification provider account identity is unavailable")
	}
	c, err := r.startCapture(captureStart{Owner: owner, After: &after, Before: &before, CapturedAt: &at, AccountFingerprint: identity.AccountFingerprint()})
	if err != nil {
		return nil, captureReference{}, err
	}
	var batches []listingBatch
	var pager listingPager
	pages := 0
	err = lister.StreamPages(ctx, func(_ []anthropic.MessageBatch, raw string) error {

		if writeErr := r.writeCapture(filepath.Join(c.dir, captureRecordName(pages)), raw); writeErr != nil {
			return &captureWriteError{writeErr}
		}
		pages++
		page, pageErr := pager.accept(raw)
		if pageErr != nil {
			return &captureWriteError{pageErr}
		}
		for _, b := range page.Data {
			if !b.CreatedAt.Before(after) && !b.CreatedAt.After(before) {
				batches = append(batches, b)
			}
		}
		return nil
	})
	if err == nil && !pager.complete() {
		err = fmt.Errorf("provider listing ended before final page")
	}
	if err = r.endCapture(c, pages, err); err != nil {
		return nil, captureReference{}, err
	}
	files, err := readRetainedDirectory(c.dir)
	if err != nil {
		return nil, captureReference{}, err
	}
	proof, err := captureProof(r.IntentDir, c.dir, files)
	return batches, proof, err
}
