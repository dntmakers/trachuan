package classify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"

	"golang.org/x/sys/unix"
)

const (
	trialPreparing  = "preparing"
	trialReserved   = "reserved"
	trialUploading  = "uploading"
	trialUploaded   = "uploaded"
	trialDispatched = "dispatched"
	trialSubmitted  = "submitted"
	trialTerminal   = "terminal"
	trialSettled    = "settled"
)

type openAITrialIdentity struct {
	Model        string `json:"model"`
	Effort       string `json:"effort,omitempty"`
	Stage        string `json:"stage"`
	Country      string `json:"country"`
	Release      string `json:"release"`
	LabelsHash   string `json:"labels_sha256"`
	RecipeHash   string `json:"recipe_sha256"`
	PriceVersion string `json:"price_version"`
	Project      string `json:"project"`
	Fingerprint  string `json:"key_fingerprint"`
}

type openAITrialState struct {
	openAITrialIdentity
	Version         int                        `json:"version"`
	Phase           string                     `json:"phase"`
	SubmissionID    string                     `json:"submission_id"`
	InputHash       string                     `json:"input_sha256"`
	PlanHash        string                     `json:"plan_sha256"`
	ReserveMicroUSD int64                      `json:"reserve_micro_usd"`
	CostMicroUSD    int64                      `json:"cost_micro_usd"`
	UnpricedResults int                        `json:"unpriced_results,omitempty"`
	FileID          string                     `json:"input_file_id,omitempty"`
	BatchID         string                     `json:"batch_id,omitempty"`
	Upload          *openAITrialUploadIdentity `json:"upload,omitempty"`
	TerminalHash    string                     `json:"terminal_sha256,omitempty"`
	TerminalCapture string                     `json:"terminal_capture,omitempty"`
	SiblingMicroUSD int64                      `json:"sibling_micro_usd,omitempty"`
	Output          *openAITrialFileBinding    `json:"output,omitempty"`
	Error           *openAITrialFileBinding    `json:"error,omitempty"`
}

func (s openAITrialState) valid() bool {
	if (s.Version != 2 && (s.Version != 1 || s.Phase != trialPreparing)) || (s.Version == 2 && s.Effort == "") || !validID(s.SubmissionID) || !validID(s.Fingerprint) || !openAIProjectValid(s.Project) || !validKey(s.LabelsHash) || !validKey(s.RecipeHash) || s.PriceVersion != openAIBatchPriceVersion || (s.Stage != "document" && s.Stage != "article") || s.Country == "" || s.Release == "" || s.ReserveMicroUSD < 0 || s.ReserveMicroUSD > OpenAITrialMaxMicroUSD || s.CostMicroUSD < 0 {
		return false
	}
	needsUpload := s.Phase != trialPreparing && s.Phase != trialReserved && s.ReserveMicroUSD > 0
	if needsUpload {
		if s.Upload == nil || *s.Upload != s.uploadIdentity() {
			return false
		}
	} else if s.Upload != nil {
		return false
	}
	if s.UnpricedResults < 0 || (s.UnpricedResults > 0 && (s.BatchID == "" || (s.Phase != trialTerminal && s.Phase != trialSettled))) {
		return false
	}
	if s.SiblingMicroUSD < 0 || s.SiblingMicroUSD > OpenAITrialMaxMicroUSD {
		return false
	}
	hasTerminal := s.Phase == trialTerminal || s.Phase == trialSettled && s.BatchID != ""
	if hasTerminal {
		if !validKey(s.TerminalHash) || !validTrialCaptureName(s.TerminalCapture, "retrieve") {
			return false
		}
	} else if s.TerminalHash != "" || s.TerminalCapture != "" || s.Output != nil || s.Error != nil {
		return false
	}
	if s.Output != nil && !s.Output.valid(s.BatchID, "output") || s.Error != nil && !s.Error.valid(s.BatchID, "error") {
		return false
	}
	if _, ok := openAIPriceForModel(s.Model); !ok {
		return false
	}
	if s.Phase == trialPreparing {
		return s.InputHash == "" && s.PlanHash == "" && s.ReserveMicroUSD == 0 && s.CostMicroUSD == 0 && s.FileID == "" && s.BatchID == ""
	}
	if !validKey(s.InputHash) || !validKey(s.PlanHash) {
		return false
	}
	switch s.Phase {
	case trialReserved, trialUploading:
		return s.FileID == "" && s.BatchID == "" && s.CostMicroUSD == 0 && (s.Phase != trialUploading || s.ReserveMicroUSD > 0)
	case trialUploaded, trialDispatched:
		return s.ReserveMicroUSD > 0 && s.CostMicroUSD == 0 && openAIUsableID(s.FileID) && s.BatchID == ""
	case trialSubmitted, trialTerminal:
		return s.ReserveMicroUSD > 0 && openAIUsableID(s.FileID) && openAIUsableID(s.BatchID)
	case trialSettled:
		return (s.ReserveMicroUSD == 0 && s.CostMicroUSD == 0 && s.FileID == "" && s.BatchID == "") || (s.ReserveMicroUSD > 0 && openAIUsableID(s.FileID) && openAIUsableID(s.BatchID))
	default:
		return false
	}
}

func trialLock(dir string) (func(), error) {
	return lockTrialDirectory(dir, unix.LOCK_EX|unix.LOCK_NB)
}

func lockTrialDirectory(dir string, mode int) (func(), error) {
	fd, err := unix.Open(dir, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open trial directory: %w", err)
	}
	for {
		err = unix.Flock(fd, mode)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("trial directory busy: %w", err)
	}
	return func() { _ = unix.Close(fd) }, nil
}

func (t *openAITrial) open(stage string) (func(), error) {
	abs, err := filepath.Abs(t.o.Out)
	if err != nil || abs == filepath.Dir(abs) {
		return nil, fmt.Errorf("invalid trial output directory")
	}
	t.o.Out = abs
	identity := openAITrialIdentity{t.o.Recipe.Model, t.o.Recipe.Effort, stage, t.o.Country, t.o.Release, t.o.LabelsHash, t.o.Recipe.Hash(), openAIBatchPriceVersion, t.client.ProjectID(), t.client.AccountFingerprint()}
	if t.client.guard([]byte(identity.Project)) != nil || t.client.guard([]byte(abs)) != nil || !openAIProjectValid(identity.Project) || !validID(identity.Fingerprint) {
		return nil, fmt.Errorf("invalid OpenAI project or fingerprint")
	}
	if err = durableDirectory(abs); err != nil {
		return nil, err
	}
	unlock, err := trialLock(abs)
	if err != nil {
		return nil, err
	}
	if err = t.load(identity); err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}

func (t *openAITrial) load(identity openAITrialIdentity) error {
	found, err := readTrialState(t.path("state.json"), &t.state)
	if err != nil {
		return err
	}
	if found {
		if t.state.Version == 1 {
			t.state.Effort = identity.Effort
		}
		if t.state.openAITrialIdentity != identity {
			return fmt.Errorf("trial state does not match stage, inputs, recipe or credentials")
		}
		if t.state.Version == 1 {
			t.state.Version = 2
			if err = t.save(); err != nil {
				return err
			}
		}
	} else {
		entries, listErr := os.ReadDir(t.o.Out)
		if listErr != nil {
			return fmt.Errorf("read trial directory: %w", listErr)
		}
		if len(entries) != 0 {
			return fmt.Errorf("nonempty trial directory has no state.json")
		}
		id, idErr := newID()
		if idErr != nil {
			return idErr
		}
		t.state = openAITrialState{openAITrialIdentity: identity, Version: 2, Phase: trialPreparing, SubmissionID: id}
		if err = t.save(); err != nil {
			return err
		}
	}
	if t.state.Phase == trialPreparing {
		if err = t.loadCounts(); err != nil {
			return err
		}
	}
	if err = syncDirectory(filepath.Dir(t.o.Out)); err != nil {
		return err
	}
	return syncDirectory(t.o.Out)
}

func (t *openAITrial) admit(summary *OpenAITrialSummary) error {
	return t.withTrialBudget(summary, func() error {
		if t.o.DryRun {
			return nil
		}
		plan, err := json.Marshal(openAITrialPlan{t.o.Recipe, t.result.Items, t.result.Families, t.nodes})
		if err != nil {
			return fmt.Errorf("encode trial plan: %w", err)
		}
		if err = t.raw("plan.json", plan); err != nil {
			return err
		}
		if err = t.raw("input.jsonl", t.input); err != nil {
			return err
		}
		t.state.InputHash, t.state.PlanHash, t.state.ReserveMicroUSD, t.state.SiblingMicroUSD, t.state.Phase = digest(t.input), digest(plan), summary.ReserveMicroUSD, summary.SiblingMicroUSD, trialReserved
		return t.save()
	})
}

func readTrialState(path string, dst *openAITrialState) (bool, error) {
	raw, found, err := readTrialFile(path)
	if err != nil || !found {
		return found, err
	}
	fields, err := batchapi.DecodeEvidenceProjection(raw, "reserve_micro_usd", "cost_micro_usd")
	if err != nil || fields["reserve_micro_usd"] == nil || fields["cost_micro_usd"] == nil || validateRetainedJSON(raw, dst) != nil || json.Unmarshal(raw, dst) != nil || !dst.valid() {
		return false, fmt.Errorf("invalid or unsupported trial state %s", path)
	}
	if dst.Phase == trialPreparing {
		traces, err := trialTraces(filepath.Dir(path), false)
		if err != nil || traces {
			return false, fmt.Errorf("preparing trial has remote evidence without an admitted state")
		}
	}
	return true, nil
}

func trialTraces(dir string, includeCounts bool) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		name := entry.Name()
		switch name {
		case "upload.json", "create.json", "terminal.json", "output.jsonl", "error.jsonl", "cost.json":
			return true, nil
		case "counts.json", "input.jsonl", "plan.json":
			if includeCounts {
				return true, nil
			}
		}
		if strings.HasSuffix(name, ".capture") || strings.HasSuffix(name, ".partial.jsonl") || (includeCounts && strings.HasPrefix(name, "count-")) {
			return true, nil
		}
	}
	return false, nil
}

func (t *openAITrial) path(name string) string { return filepath.Join(t.o.Out, name) }

func (t *openAITrial) save() error { return t.writeJSON("state.json", t.state) }

func readTrialFile(path string) ([]byte, bool, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open trial %s: %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > openAITrialResultLimit+4096 {
		return nil, false, fmt.Errorf("invalid trial file %s", path)
	}
	raw, err := io.ReadAll(io.LimitReader(f, openAITrialResultLimit+4097))
	if err != nil || len(raw) > openAITrialResultLimit+4096 {
		return nil, false, fmt.Errorf("cannot read trial file %s", path)
	}
	return raw, true, nil
}

func readTrialJSON(path string, dst any) (bool, error) {
	raw, found, err := readTrialFile(path)
	if err != nil || !found {
		return found, err
	}
	if validateRetainedJSON(raw, dst) != nil || json.Unmarshal(raw, dst) != nil {
		return false, fmt.Errorf("invalid trial record %s", path)
	}
	return true, nil
}

func writeTrialJSON(path string, value any) error {
	return writeTrialFile(path, func(w io.Writer) error {
		e := json.NewEncoder(w)
		e.SetIndent("", "  ")
		return e.Encode(value)
	})
}

func writeTrialFile(path string, write func(io.Writer) error) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".trial-")
	if err != nil {
		return fmt.Errorf("create trial file: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	writeErr := write(f)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if err = errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("persist trial file: %w", err)
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("publish trial file: %w", err)
	}
	return syncDirectory(dir)
}
