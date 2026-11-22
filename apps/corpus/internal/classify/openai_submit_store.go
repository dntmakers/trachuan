package classify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/openaibatch"
	"golang.org/x/sys/unix"
)

type openAISubmitDisk struct {
	root       string
	guard      *openAICreatorGuard
	now        func() time.Time
	syncDir    func(int) error
	started    bool
	poisoned   bool
	step       int
	intentHash string
}

type submitRecord struct {
	Provider     string    `json:"provider"`
	Intent       string    `json:"intent_id"`
	ItemsHash    string    `json:"items_hash"`
	Project      string    `json:"project"`
	Fingerprint  string    `json:"fingerprint"`
	At           time.Time `json:"at"`
	InputHash    string    `json:"input_hash,omitempty"`
	ResponseHash string    `json:"response_hash,omitempty"`
	IDs          []string  `json:"ids,omitempty"`
	FileID       string    `json:"file_id,omitempty"`
	SubmissionID string    `json:"submission_id,omitempty"`
	BatchID      string    `json:"batch_id,omitempty"`
	Status       int       `json:"status"`
	Body         []byte    `json:"body,omitempty"`
}

var submitNames = []string{"input.json", "upload-response.json", "upload-receipt.json", "create-dispatch.json", "create-response.json", "batch-receipt.json"}

func submitRead(dir int, name string) ([]byte, error) {
	fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid submit record")
	}
	b, err := io.ReadAll(io.LimitReader(f, 8<<20+1))
	if err != nil || len(b) > 8<<20 {
		return nil, fmt.Errorf("invalid submit record size")
	}
	return b, nil
}

func (d *openAISubmitDisk) sync(fd int) error {
	if d.syncDir != nil {
		return d.syncDir(fd)
	}
	return unix.Fsync(fd)
}

func (d *openAISubmitDisk) with(s Submission, create bool, fn func(int) error) error {
	if d.guard == nil || !validID(s.ID) || s.Provider != "openai" || !validID(s.AccountFingerprint) || s.State != "preparing" || s.BatchID != "" || s.SubmittedAt != nil || s.DeadlineAt != nil || s.Settled || s.CostMicroUSD != 0 || validateRecovery(s, s.ID) != nil {
		return fmt.Errorf("invalid submission")
	}
	st, err := openCountRoot(d.root)
	if err != nil {
		return err
	}
	defer st.close()
	for {
		err = unix.Flock(st.root, unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return err
	}
	if err = verifyOpenAICreatorFD(st.root, s.ID, d.guard); err != nil {
		return err
	}
	sub, err := countOpenDir(st.root, "submissions", false)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(sub) }()
	if err = d.sync(sub); err != nil {
		return err
	}
	if err = submitNamespace(sub, s.ID); err != nil {
		return err
	}
	raw, err := submitRead(sub, s.ID+".json")
	if err != nil {
		return err
	}
	canonical, err := json.Marshal(s)
	if err != nil || !bytes.Equal(raw, canonical) || d.started && digest(raw) != d.intentHash {
		return fmt.Errorf("intent mismatch")
	}
	if !d.started {
		d.intentHash = digest(raw)
	}
	if err = submitOtherEvidence(st.root, s.ID); err != nil {
		return err
	}
	if err = submitResultEvidence(st.root, s.ID); err != nil {
		return err
	}
	dir := st.root
	var dirs []int
	defer func() {
		for i := len(dirs) - 1; i >= 0; i-- {
			_ = unix.Close(dirs[i])
		}
	}()
	for _, name := range []string{"evidence", s.ID, "openai-submit"} {
		next, e := countOpenDir(dir, name, create)
		if errors.Is(e, unix.ENOENT) && !create {
			return fn(-1)
		}
		if e != nil {
			return e
		}
		dirs = append(dirs, next)
		dir = next
	}
	for _, fd := range append([]int{st.parent, st.root}, dirs...) {
		if err = d.sync(fd); err != nil {
			return err
		}
	}
	return fn(dir)
}

func (d *openAISubmitDisk) scope(s Submission) submitRecord {
	return submitRecord{Provider: s.Provider, Intent: s.ID, ItemsHash: s.ItemsHash, Project: s.ProjectID, Fingerprint: s.AccountFingerprint}
}
func (d *openAISubmitDisk) read(dir int, s Submission, index int) (submitRecord, error) {
	var r submitRecord
	if dir < 0 {
		return r, unix.ENOENT
	}
	raw, err := submitRead(dir, submitNames[index])
	if err != nil {
		return r, err
	}
	if validateRetainedJSON(raw, &r) != nil || json.Unmarshal(raw, &r) != nil {
		return r, fmt.Errorf("invalid submit record")
	}
	scope := d.scope(s)
	if r.Provider != scope.Provider || r.Intent != scope.Intent || r.ItemsHash != scope.ItemsHash || r.Project != scope.Project || r.Fingerprint != scope.Fingerprint || r.At.Before(s.CreatedAt) || r.At.Location() != time.UTC {
		return r, fmt.Errorf("submit scope mismatch")
	}
	return r, nil
}

func (d *openAISubmitDisk) history(dir int, s Submission, n int) ([]submitRecord, error) {
	if err := submitPrefix(dir, n); err != nil {
		return nil, err
	}
	out := make([]submitRecord, n)
	for i := 0; i < n; i++ {
		r, err := d.read(dir, s, i)
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	for i := n; i < len(submitNames); i++ {
		_, err := d.read(dir, s, i)
		if !errors.Is(err, unix.ENOENT) {
			return nil, fmt.Errorf("submit record already exists or invalid")
		}
	}
	if n > 0 {
		var ids []string
		for i, item := range s.Items {
			if !item.Local {
				ids = append(ids, customID(s.ID, i))
			}
		}
		if !validKey(out[0].InputHash) || !reflect.DeepEqual(ids, out[0].IDs) {
			return nil, fmt.Errorf("invalid input scope")
		}
	}
	if n > 2 {
		id, err := openAIUploadID(out[1].Body)
		if err != nil || out[1].Status < 200 || out[1].Status >= 300 || out[1].Status == 206 || out[2].FileID != id || out[2].ResponseHash != digest(out[1].Body) || out[2].InputHash != out[0].InputHash {
			return nil, fmt.Errorf("invalid upload receipt")
		}
	}
	if n > 3 && (out[3].FileID != out[2].FileID || out[3].SubmissionID != s.ID || out[3].InputHash != out[0].InputHash) {
		return nil, fmt.Errorf("invalid dispatch")
	}
	return out, nil
}

func (d *openAISubmitDisk) write(dir int, name string, r submitRecord) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return publishEvidenceOnce(dir, dir, name, raw, d.sync)
}

func publishEvidenceOnce(stagingParent, dir int, name string, raw []byte, syncDir func(int) error) error {
	stage, err := countOpenDir(stagingParent, ".staging", true)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(stage) }()
	id, err := newID()
	if err != nil {
		return err
	}
	fd, err := unix.Openat(stage, id, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Unlinkat(stage, id, 0) }()
	f := os.NewFile(uintptr(fd), id)
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	e := f.Close()
	if err != nil {
		return err
	}
	if e != nil {
		return e
	}
	if err = unix.Linkat(stage, id, dir, name, 0); err != nil {
		return err
	}
	if err = syncDir(dir); err != nil {
		return err
	}
	return syncDir(stage)
}
func (d *openAISubmitDisk) advance(s Submission, n int, build func([]submitRecord) (submitRecord, error)) error {
	if !d.started || d.poisoned || d.step != n {
		d.poisoned = true
		return fmt.Errorf("invalid evidence advancement")
	}
	err := d.with(s, true, func(dir int) error {
		prev, err := d.history(dir, s, n)
		if err != nil {
			return err
		}
		r, err := build(prev)
		if err != nil {
			return err
		}
		scope := d.scope(s)
		r.Provider = scope.Provider
		r.Intent = scope.Intent
		r.ItemsHash = scope.ItemsHash
		r.Project = scope.Project
		r.Fingerprint = scope.Fingerprint
		if d.now == nil {
			return fmt.Errorf("missing clock")
		}
		r.At = d.now().UTC()
		if r.At.Before(s.CreatedAt) {
			return fmt.Errorf("invalid clock")
		}
		return d.write(dir, submitNames[n], r)
	})
	if err != nil {
		d.poisoned = true
		return err
	}
	d.step++
	return nil
}
func (d *openAISubmitDisk) Check(s Submission) error {
	if d.started || d.poisoned {
		d.poisoned = true
		return fmt.Errorf("invalid evidence check")
	}
	err := d.with(s, false, func(dir int) error { _, err := d.history(dir, s, 0); return err })
	if err != nil {
		d.poisoned = true
		return err
	}
	d.started = true
	return nil
}
func (d *openAISubmitDisk) Input(s Submission, hash string, ids []string) error {
	return d.advance(s, 0, func(_ []submitRecord) (submitRecord, error) {
		var expected []string
		for i, item := range s.Items {
			if !item.Local {
				expected = append(expected, customID(s.ID, i))
			}
		}
		if !validKey(hash) || !reflect.DeepEqual(ids, expected) {
			return submitRecord{}, fmt.Errorf("invalid input")
		}
		return submitRecord{InputHash: hash, IDs: ids}, nil
	})
}
func (d *openAISubmitDisk) UploadResponse(s Submission, resp openaibatch.Response) error {
	return d.response(s, 1, resp)
}
func (d *openAISubmitDisk) CreateResponse(s Submission, resp openaibatch.Response) error {
	return d.response(s, 4, resp)
}
func (d *openAISubmitDisk) response(s Submission, n int, resp openaibatch.Response) error {
	return d.advance(s, n, func(_ []submitRecord) (submitRecord, error) {
		if len(resp.Body) > 4<<20 {
			return submitRecord{}, fmt.Errorf("oversize response")
		}
		return submitRecord{Status: resp.Status, Body: resp.Body}, nil
	})
}
func (d *openAISubmitDisk) UploadVerified(s Submission, id, hash string) error {
	return d.advance(s, 2, func(p []submitRecord) (submitRecord, error) {
		got, e := openAIUploadID(p[1].Body)
		if e != nil || got != id || p[1].Status < 200 || p[1].Status >= 300 || p[1].Status == 206 || hash != p[0].InputHash {
			return submitRecord{}, fmt.Errorf("invalid upload")
		}
		return submitRecord{FileID: id, InputHash: hash, ResponseHash: digest(p[1].Body)}, nil
	})
}
func (d *openAISubmitDisk) Dispatch(s Submission, file, id string) error {
	return d.advance(s, 3, func(p []submitRecord) (submitRecord, error) {
		if file != p[2].FileID || id != s.ID {
			return submitRecord{}, fmt.Errorf("invalid dispatch")
		}
		return submitRecord{FileID: file, SubmissionID: id, InputHash: p[0].InputHash}, nil
	})
}
func (d *openAISubmitDisk) Receipt(s Submission, id string) error {
	return d.advance(s, 5, func(p []submitRecord) (submitRecord, error) {
		got, e := openAICreateID(p[4].Body, p[3].FileID, s.ID)
		if e != nil || got != id || p[4].Status < 200 || p[4].Status >= 300 || p[4].Status == 206 {
			return submitRecord{}, fmt.Errorf("invalid receipt")
		}
		return submitRecord{BatchID: id, FileID: p[3].FileID, SubmissionID: s.ID, InputHash: p[0].InputHash, ResponseHash: digest(p[4].Body)}, nil
	})
}
