package classify

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"golang.org/x/sys/unix"
)

func (t *openAITrial) recoverCreate() error {
	capture, raw, found, err := t.readCapture("create.capture")
	if err != nil {
		return err
	}
	if !found || !capture.usable() {
		return fmt.Errorf("OpenAI create outcome unknown; list batches in the original project and match metadata.submission_id=%s and the input_file_id recorded in state.json; do not create another batch", t.state.SubmissionID)
	}
	id, err := openAICreateID(raw, t.state.FileID, t.state.SubmissionID)
	if err != nil {
		return fmt.Errorf("OpenAI create receipt unusable; list batches in the original project and match metadata.submission_id=%s and input_file_id in state.json", t.state.SubmissionID)
	}
	if err = t.raw("create.json", raw); err != nil {
		return err
	}
	t.state.BatchID, t.state.Phase = id, trialSubmitted
	return t.save()
}

func (t *openAITrial) runBatch(ctx context.Context) error {
	if err := t.ensureUpload(ctx); err != nil {
		return err
	}
	if err := t.ensureCreate(ctx); err != nil {
		return err
	}
	batch, err := t.pollBatch(ctx)
	if err != nil {
		return err
	}
	return t.collect(ctx, batch)
}

type openAITrialUploadIdentity struct {
	SubmissionID string `json:"submission_id"`
	Project      string `json:"project"`
	Fingerprint  string `json:"key_fingerprint"`
	InputHash    string `json:"input_sha256"`
}

func (s openAITrialState) uploadIdentity() openAITrialUploadIdentity {
	return openAITrialUploadIdentity{s.SubmissionID, s.Project, s.Fingerprint, s.InputHash}
}

func (t *openAITrial) ensureUpload(ctx context.Context) error {
	identity := t.state.uploadIdentity()
	if t.state.Phase == trialReserved {
		if _, found, err := readTrialFile(t.path("upload.capture")); err != nil || found {
			return fmt.Errorf("unexpected OpenAI upload capture before dispatch")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		t.state.Phase, t.state.Upload = trialUploading, &identity
		if err := t.save(); err != nil {
			return err
		}
		call, cancel := context.WithTimeout(ctx, 5*time.Minute)
		response, callErr := t.client.Upload(call, bytes.NewReader(t.input))
		cancel()
		if err := t.capture("upload.capture", openAITrialCapture{Status: response.Status, Complete: callErr == nil, Upload: &identity}, response.Body); err != nil {
			return err
		}
	}
	capture, raw, found, err := t.readCapture("upload.capture")
	if err != nil {
		return err
	}
	if !found || !capture.usable() {
		return fmt.Errorf("OpenAI upload outcome unknown; reconcile the upload in the original project before any replacement; do not upload again")
	}
	if t.state.Upload == nil || *t.state.Upload != identity || capture.Upload == nil || *capture.Upload != identity {
		return fmt.Errorf("OpenAI upload receipt differs from dispatch identity")
	}
	id, err := openAIUploadID(raw)
	if err != nil {
		return fmt.Errorf("OpenAI upload receipt unusable; reconcile the upload in the original project; do not upload again")
	}
	if t.state.Phase == trialUploading {
		if err := t.raw("upload.json", raw); err != nil {
			return err
		}
		t.state.FileID, t.state.Phase = id, trialUploaded
		return t.save()
	}
	if id != t.state.FileID {
		return fmt.Errorf("OpenAI upload receipt differs from trial state")
	}
	return nil
}

func (t *openAITrial) ensureCreate(ctx context.Context) error {
	if t.state.Phase == trialUploaded {
		if _, found, err := readTrialFile(t.path("create.capture")); err != nil || found {
			return fmt.Errorf("unexpected OpenAI create capture before dispatch")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		budget := OpenAITrialSummary{ReserveMicroUSD: t.state.ReserveMicroUSD}
		if err := t.withTrialBudget(&budget, func() error {
			t.state.Phase, t.state.SiblingMicroUSD = trialDispatched, budget.SiblingMicroUSD
			return t.save()
		}); err != nil {
			return err
		}
		call, cancel := context.WithTimeout(ctx, time.Minute)
		response, callErr := t.client.Create(call, t.state.FileID, t.state.SubmissionID)
		cancel()
		if err := t.captureBatchObservation("create.capture", openAITrialCapture{Status: response.Status, Complete: callErr == nil}, response.Body); err != nil {
			return err
		}
	}
	if t.state.Phase == trialDispatched {
		return t.recoverCreate()
	}
	capture, raw, found, err := t.readCapture("create.capture")
	if err != nil || !found || !capture.usable() {
		return fmt.Errorf("missing retained OpenAI create receipt")
	}
	id, err := openAICreateID(raw, t.state.FileID, t.state.SubmissionID)
	if err != nil || id != t.state.BatchID {
		return fmt.Errorf("OpenAI create receipt differs from trial state")
	}
	return nil
}

func (t *openAITrial) pollBatch(ctx context.Context) (openAIBatchTerminal, error) {
	identity := openAIBatchIdentity{BatchID: t.state.BatchID, SubmissionID: t.state.SubmissionID, InputFileID: t.state.FileID}
	present, exists, err := readTrialFile(t.path("terminal.json"))
	if err != nil {
		return openAIBatchTerminal{}, err
	}
	var raw []byte
	var name string
	if t.state.Phase == trialSubmitted {
		if exists {
			return openAIBatchTerminal{}, fmt.Errorf("unbound trial terminal receipt")
		}
		raw, name, err = t.retrieveTerminal(ctx)
		if err != nil {
			return openAIBatchTerminal{}, err
		}
	} else {
		meta, captured, found, captureErr := t.readCapture(t.state.TerminalCapture)
		if captureErr != nil || !found || !meta.usable() || meta.BatchID != identity.BatchID || meta.Hash != t.state.TerminalHash || (exists && digest(present) != meta.Hash) {
			return openAIBatchTerminal{}, fmt.Errorf("trial terminal receipt differs from retained capture")
		}
		raw = captured
	}
	batch, err := parseOpenAIBatchTerminal(raw, identity)
	if err != nil {
		return batch, err
	}
	if t.state.Phase == trialSubmitted {
		t.state.Phase, t.state.TerminalHash, t.state.TerminalCapture = trialTerminal, digest(raw), name
		if err = t.save(); err != nil {
			return batch, err
		}
	}
	if !exists {
		if err = t.raw("terminal.json", raw); err != nil {
			return batch, err
		}
	}
	return batch, nil
}

func (t *openAITrial) retrieveTerminal(ctx context.Context) ([]byte, string, error) {
	poll, stop := context.WithTimeout(ctx, 10*time.Minute)
	defer stop()
	for {
		call, cancel := context.WithTimeout(poll, time.Minute)
		response, callErr := t.client.Retrieve(call, t.state.BatchID)
		cancel()
		attempt, err := newID()
		if err != nil {
			return nil, "", err
		}
		capture := openAITrialCapture{Status: response.Status, Complete: callErr == nil, BatchID: t.state.BatchID}
		name := "retrieve-" + attempt + ".capture"
		if err = t.captureBatchObservation(name, capture, response.Body); err != nil {
			return nil, "", err
		}
		if !capture.usable() {
			return nil, "", fmt.Errorf("OpenAI trial retrieve failed; rerun the same command to resume")
		}
		status, terminal, err := trialBatchStatus(response.Body)
		if err != nil {
			return nil, "", err
		}
		t.progress(status)
		if terminal {
			return response.Body, name, nil
		}
		timer := time.NewTimer(10 * time.Second)
		select {
		case <-poll.Done():
			timer.Stop()
			return nil, "", fmt.Errorf("OpenAI trial polling stopped; rerun the same command to resume: %w", poll.Err())
		case <-timer.C:
		}
	}
}

func (t *openAITrial) progress(status string) {
	if t.o.Progress != nil {
		_, _ = fmt.Fprintf(t.o.Progress, "OpenAI batch status: %s\n", status)
	}
}

func (t *openAITrial) captureBatchObservation(name string, capture openAITrialCapture, raw []byte) error {
	unlock, err := lockTrialDirectory(filepath.Dir(t.o.Out), unix.LOCK_EX)
	if err != nil {
		return err
	}
	defer unlock()
	return t.capture(name, capture, raw)
}

func trialBatchStatus(raw []byte) (status string, terminal bool, err error) {
	fields, err := batchapi.DecodeEvidenceProjection(raw, "status")
	status, statusErr := evidenceString(fields["status"])
	if err != nil || statusErr != nil {
		return "", false, fmt.Errorf("invalid OpenAI batch status")
	}
	switch status {
	case "completed", "failed", "expired", "cancelled":
		return status, true, nil
	case "validating", "in_progress", "finalizing", "cancelling":
		return status, false, nil
	default:
		return "", false, fmt.Errorf("unknown OpenAI batch status")
	}
}
