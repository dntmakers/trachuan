package classify

import (
	"bytes"
	"fmt"
	"strings"
)

func (d packDir) readCapture(name string) (openAITrialCapture, []byte, error) {
	data, err := d.required(name)
	if err != nil {
		return openAITrialCapture{}, nil, err
	}
	c, raw, err := decodeTrialCapture(data)
	if err != nil {
		return c, nil, fmt.Errorf("invalid %s: %w", name, err)
	}
	return c, raw, nil
}

func (d packDir) capture(name string) (openAITrialCapture, []byte, error) {
	c, raw, err := d.readCapture(name)
	if err != nil {
		return c, nil, err
	}
	if !c.usable() {
		return c, nil, fmt.Errorf("incomplete %s", name)
	}
	return c, raw, nil
}

func (d packDir) mirror(name string, raw []byte) error {
	present, found, err := d.read(name)
	if err != nil {
		return err
	}
	if found && (raw == nil || !bytes.Equal(present, raw)) {
		return fmt.Errorf("%s differs from its capture", name)
	}
	return nil
}

func (d packDir) receipts(s openAITrialState) (openAIBatchTerminal, openAIBatchCapture, error) {
	batch, err := d.terminal(s)
	if err != nil {
		return batch, openAIBatchCapture{}, err
	}
	names, err := d.names()
	if err != nil {
		return batch, openAIBatchCapture{}, fmt.Errorf("list result captures: %w", err)
	}
	capture := openAIBatchCapture{BatchID: s.BatchID, OutputFileID: batch.OutputFileID, ErrorFileID: batch.ErrorFileID}
	capture.Output, err = d.resultCapture(s.BatchID, "output", batch.OutputFileID, s.Output, names)
	if err != nil {
		return batch, capture, err
	}
	capture.Error, err = d.resultCapture(s.BatchID, "error", batch.ErrorFileID, s.Error, names)
	return batch, capture, err
}

func (d packDir) terminal(s openAITrialState) (openAIBatchTerminal, error) {
	var batch openAIBatchTerminal
	c, raw, err := d.capture("upload.capture")
	if err != nil {
		return batch, err
	}
	id, parseErr := openAIUploadID(raw)
	if parseErr != nil || packCheckJSON(raw) != nil || c.Upload == nil || *c.Upload != s.uploadIdentity() || c.BatchID != "" || c.FileID != "" || id != s.FileID {
		return batch, fmt.Errorf("upload.capture does not match state.json upload identity")
	}
	if err = d.mirror("upload.json", raw); err != nil {
		return batch, err
	}
	c, raw, err = d.capture("create.capture")
	if err != nil {
		return batch, err
	}
	id, parseErr = openAICreateID(raw, s.FileID, s.SubmissionID)
	if parseErr != nil || packCheckJSON(raw) != nil || c.Upload != nil || c.BatchID != "" || c.FileID != "" || id != s.BatchID {
		return batch, fmt.Errorf("create.capture does not match state.json batch identity")
	}
	if err = d.mirror("create.json", raw); err != nil {
		return batch, err
	}
	c, raw, err = d.capture(s.TerminalCapture)
	if err != nil {
		return batch, err
	}
	if packCheckJSON(raw) != nil || c.Hash != s.TerminalHash || c.BatchID != s.BatchID || c.FileID != "" || c.Upload != nil {
		return batch, fmt.Errorf("%s does not match state.json terminal identity", s.TerminalCapture)
	}
	batch, err = parseOpenAIBatchTerminal(raw, openAIBatchIdentity{BatchID: s.BatchID, SubmissionID: s.SubmissionID, InputFileID: s.FileID})
	if err != nil {
		return batch, fmt.Errorf("invalid %s: %w", s.TerminalCapture, err)
	}
	return batch, d.mirror("terminal.json", raw)
}

func (d packDir) resultCapture(batch, part, id string, binding *openAITrialFileBinding, names []string) (*openAIFileCapture, error) {
	name := part + ".jsonl"
	if id == "" {
		if binding != nil {
			return nil, fmt.Errorf("unexpected state.json %s binding", part)
		}
		if err := d.checkResultCaptureCopies(batch, part, id, "", nil, names); err != nil {
			return nil, err
		}
		return nil, d.mirror(name, nil)
	}
	if binding == nil || !binding.valid(batch, part) || binding.FileID != id {
		return nil, fmt.Errorf("invalid state.json %s binding", part)
	}
	c, raw, err := d.capture(binding.Capture)
	if err != nil {
		return nil, err
	}
	if c.Status != 200 || c.BatchID != batch || c.FileID != id || c.Hash != binding.Hash || c.Upload != nil {
		return nil, fmt.Errorf("%s does not match state.json %s binding", binding.Capture, part)
	}
	if err = d.checkResultCaptureCopies(batch, part, id, binding.Capture, raw, names); err != nil {
		return nil, err
	}
	if err = d.mirror(name, raw); err != nil {
		return nil, err
	}
	result := trialFileCapture(raw)
	for i, line := range result.Lines {
		if packCheckJSON(line) != nil {
			return nil, fmt.Errorf("invalid or ambiguous JSON in %s line %d", binding.Capture, i+1)
		}
	}
	return result, nil
}

func (d packDir) checkResultCaptureCopies(batch, part, file, selected string, raw []byte, names []string) error {
	for _, name := range names {
		if name == selected || !strings.HasPrefix(name, part+"-") || !strings.HasSuffix(name, ".capture") {
			continue
		}
		capture, payload, err := d.readCapture(name)
		if err != nil {
			return err
		}
		if !capture.Complete || (capture.BatchID != batch && (file == "" || capture.FileID != file)) {
			continue
		}
		if selected == "" || capture.Status != 200 || capture.BatchID != batch || capture.FileID != file || capture.Upload != nil || !bytes.Equal(raw, payload) {
			return fmt.Errorf("%s conflicts with the verified %s capture", name, part)
		}
	}
	return nil
}

func (d packDir) noBatchEvidence() error {
	names, err := d.names()
	if err != nil {
		return err
	}
	for _, name := range names {
		switch name {
		case "upload.json", "create.json", "terminal.json", "output.jsonl", "error.jsonl":
			return fmt.Errorf("local-only trial has unexpected %s", name)
		}
		if strings.HasSuffix(name, ".capture") || strings.HasSuffix(name, ".partial.jsonl") {
			return fmt.Errorf("local-only trial has unexpected batch capture")
		}
	}
	return nil
}
