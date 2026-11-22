package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/openaibatch"
	"golang.org/x/sys/unix"
)

func TestOpenAICreatorFence(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "submissions"), 0700); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("b", 32)
	r := &Runner{IntentDir: root}
	guard, err := r.lockOpenAICreator(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.close() }()
	if _, err = r.lockCreator(id); err == nil {
		t.Fatal("existing creator lock did not conflict")
	}
	if err = verifyOpenAICreator(root, id, guard); err != nil {
		t.Fatal(err)
	}
	if err = verifyOpenAICreator(root, strings.Repeat("c", 32), guard); err == nil {
		t.Fatal("wrong id accepted")
	}
	path := filepath.Join(root, "submissions", id+".creator.lock")
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = verifyOpenAICreator(root, id, guard); err == nil {
		t.Fatal("replacement accepted")
	}
	if err = guard.close(); err != nil {
		t.Fatal(err)
	}
	if err = verifyOpenAICreator(root, id, guard); err == nil {
		t.Fatal("closed guard accepted")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(root, "other"), path); err != nil {
		t.Fatal(err)
	}
	if _, err = r.lockOpenAICreator(id); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestOpenAISubmitDiskEvidence(t *testing.T) {
	s, bodies, client, events := submitFixture(t)
	s.State = "preparing"
	s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	root := t.TempDir()
	r := &Runner{IntentDir: root}
	if err := r.writeIntent(s); err != nil {
		t.Fatal(err)
	}
	guard, err := r.lockOpenAICreator(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.close() }()
	sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
	id, err := openAISubmit(context.Background(), s, bodies, client, sink)
	if err != nil || id != "batch-test" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	dir := filepath.Join(root, "evidence", s.ID, "openai-submit")
	records := make(map[string]submitRecord)
	for _, name := range submitNames {
		raw, readErr := os.ReadFile(filepath.Join(dir, name))
		var record submitRecord
		if readErr != nil || json.Unmarshal(raw, &record) != nil {
			t.Fatalf("invalid %s: %v", name, readErr)
		}
		if record.Provider != s.Provider || record.Intent != s.ID || record.ItemsHash != s.ItemsHash || record.Project != s.ProjectID || record.Fingerprint != s.AccountFingerprint || !record.At.Equal(s.CreatedAt) {
			t.Fatalf("invalid scope %s: %+v", name, record)
		}
		records[name] = record
	}
	input := records["input.json"]
	if input.InputHash != digest(client.wire) || !reflect.DeepEqual(input.IDs, []string{customID(s.ID, 0), customID(s.ID, 2)}) {
		t.Fatalf("invalid input: %+v", input)
	}
	for _, name := range []string{"upload-receipt.json", "create-dispatch.json", "batch-receipt.json"} {
		if records[name].FileID != "file-test" || records[name].InputHash != input.InputHash {
			t.Fatalf("invalid file binding %s", name)
		}
	}
	if records["upload-receipt.json"].ResponseHash != digest(client.upload.Body) || records["batch-receipt.json"].ResponseHash != digest(client.create.Body) {
		t.Fatal("response digest not bound")
	}
	if records["create-dispatch.json"].SubmissionID != s.ID || records["batch-receipt.json"].SubmissionID != s.ID || records["batch-receipt.json"].BatchID != "batch-test" {
		t.Fatal("invalid batch binding")
	}
	*events = nil
	if _, err = openAISubmit(context.Background(), s, bodies, client, sink); err == nil || len(*events) != 0 {
		t.Fatalf("retry err=%v events=%v", err, *events)
	}
}

func TestOpenAISubmitDiskRawResponseAndFailure(t *testing.T) {
	for _, create := range []bool{false, true} {
		t.Run(fmt.Sprint(create), func(t *testing.T) {
			s, b, client, events := submitFixture(t)
			s.State = "preparing"
			s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			root := t.TempDir()
			r := &Runner{IntentDir: root}
			if err := r.writeIntent(s); err != nil {
				t.Fatal(err)
			}
			guard, err := r.lockOpenAICreator(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = guard.close() }()
			sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
			bad := openaibatch.Response{Status: 200, Body: []byte{0xff, '{'}}
			name := "upload-response.json"
			if create {
				client.create = bad
				client.createErr = errors.New("truncated")
				name = "create-response.json"
			} else {
				client.upload = bad
				client.uploadErr = errors.New("truncated")
			}
			if _, err = openAISubmit(context.Background(), s, b, client, sink); err == nil {
				t.Fatal("accepted invalid response")
			}
			raw, err := os.ReadFile(filepath.Join(root, "evidence", s.ID, "openai-submit", name))
			if err != nil {
				t.Fatal(err)
			}
			var record submitRecord
			if err = json.Unmarshal(raw, &record); err != nil || record.Status != 200 || !bytes.Equal(record.Body, bad.Body) {
				t.Fatalf("record=%+v err=%v", record, err)
			}
			*events = nil
			if _, err = openAISubmit(context.Background(), s, b, client, sink); err == nil || len(*events) != 0 {
				t.Fatalf("retry err=%v events=%v", err, *events)
			}
		})
	}
}

func TestOpenAISubmitDiskFenceAndDurability(t *testing.T) {
	for _, mode := range []string{"closed", "wrong-intent", "sync"} {
		t.Run(mode, func(t *testing.T) {
			s, b, client, events := submitFixture(t)
			s.State = "preparing"
			s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			root := t.TempDir()
			r := &Runner{IntentDir: root}
			if err := r.writeIntent(s); err != nil {
				t.Fatal(err)
			}
			guard, err := r.lockOpenAICreator(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = guard.close() }()
			sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
			switch mode {
			case "closed":
				if err = guard.close(); err != nil {
					t.Fatal(err)
				}
			case "wrong-intent":
				guard.id = strings.Repeat("c", 32)
			case "sync":
				sink.syncDir = func(int) error { return errors.New("sync failure") }
			}
			if _, err = openAISubmit(context.Background(), s, b, client, sink); err == nil || len(*events) != 0 {
				t.Fatalf("err=%v events=%v", err, *events)
			}
		})
	}
}

func TestOpenAISubmitDiskPublishedButUnsyncedRefusesRetry(t *testing.T) {
	s, b, client, events := submitFixture(t)
	s.State = "preparing"
	s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	root := t.TempDir()
	r := &Runner{IntentDir: root}
	if err := r.writeIntent(s); err != nil {
		t.Fatal(err)
	}
	guard, err := r.lockOpenAICreator(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.close() }()
	sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }, syncDir: func(fd int) error {
		if _, statErr := os.Stat(filepath.Join(root, "evidence", s.ID, "openai-submit", "input.json")); statErr == nil {
			return errors.New("publication sync failed")
		}
		return unix.Fsync(fd)
	}}
	if _, err = openAISubmit(context.Background(), s, b, client, sink); err == nil || len(*events) != 0 {
		t.Fatalf("err=%v events=%v", err, *events)
	}
	if _, err = os.Stat(filepath.Join(root, "evidence", s.ID, "openai-submit", "input.json")); err != nil {
		t.Fatal(err)
	}
	sink.syncDir = nil
	*events = nil
	if _, err = openAISubmit(context.Background(), s, b, client, sink); err == nil || len(*events) != 0 {
		t.Fatalf("retry err=%v events=%v", err, *events)
	}
}

func TestOpenAISubmitDiskRejectsSymlink(t *testing.T) {
	for _, target := range []string{"evidence", "record"} {
		t.Run(target, func(t *testing.T) {
			s, b, client, events := submitFixture(t)
			s.State = "preparing"
			s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			root := t.TempDir()
			r := &Runner{IntentDir: root}
			if err := r.writeIntent(s); err != nil {
				t.Fatal(err)
			}
			guard, err := r.lockOpenAICreator(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = guard.close() }()
			path := filepath.Join(root, "evidence")
			if target == "record" {
				path = filepath.Join(path, s.ID, "openai-submit")
				if err = os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(path, "input.json")
			}
			if err = os.Symlink(t.TempDir(), path); err != nil {
				t.Fatal(err)
			}
			sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
			if _, err = openAISubmit(context.Background(), s, b, client, sink); err == nil || len(*events) != 0 {
				t.Fatalf("err=%v events=%v", err, *events)
			}
		})
	}
}

func TestOpenAISubmitDiskRejectsChangedIntentAndPriorRecord(t *testing.T) {
	for _, mode := range []string{"intent", "record"} {
		t.Run(mode, func(t *testing.T) {
			s, b, client, events := submitFixture(t)
			s.State = "preparing"
			s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			root := t.TempDir()
			r := &Runner{IntentDir: root}
			if err := r.writeIntent(s); err != nil {
				t.Fatal(err)
			}
			guard, err := r.lockOpenAICreator(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = guard.close() }()
			sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
			if mode == "intent" {
				s.ProjectID = "proj-other"
			} else {
				dir := filepath.Join(root, "evidence", s.ID, "openai-submit")
				if err = os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, "upload-response.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err = sink.Check(s); err == nil {
				t.Fatal("accepted changed intent or partial record")
			}
			if _, err = openAISubmit(context.Background(), s, b, client, sink); err == nil || len(*events) != 0 {
				t.Fatalf("err=%v events=%v", err, *events)
			}
		})
	}
}

func TestOpenAISubmitDiskControlLimit(t *testing.T) {
	s, _, _, _ := submitFixture(t)
	s.State = "preparing"
	s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	root := t.TempDir()
	r := &Runner{IntentDir: root}
	if err := r.writeIntent(s); err != nil {
		t.Fatal(err)
	}
	guard, err := r.lockOpenAICreator(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.close() }()
	sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
	if err = sink.Check(s); err != nil {
		t.Fatal(err)
	}
	if err = sink.Input(s, strings.Repeat("a", 64), []string{customID(s.ID, 0), customID(s.ID, 2)}); err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte{0xff}, 4<<20)
	if err = sink.UploadResponse(s, openaibatch.Response{Status: 503, Body: body}); err != nil {
		t.Fatal(err)
	}
	if err = sink.with(s, false, func(dir int) error {
		record, e := sink.read(dir, s, 1)
		if e != nil {
			return e
		}
		if record.Status != 503 || !bytes.Equal(record.Body, body) {
			return errors.New("body mismatch")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAISubmitDiskRejectsExistingCreatorTrace(t *testing.T) {
	s, _, _, _ := submitFixture(t)
	s.State = "preparing"
	s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	root := t.TempDir()
	r := &Runner{IntentDir: root}
	if err := r.writeIntent(s); err != nil {
		t.Fatal(err)
	}
	guard, err := r.lockOpenAICreator(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.close() }()
	if err = os.WriteFile(filepath.Join(root, "submissions", s.ID+".creator.dispatch"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
	if err = sink.Check(s); err == nil {
		t.Fatal("accepted earlier creator dispatch")
	}
}

func TestOpenAISubmitDiskRejectsCrossPathEvidence(t *testing.T) {
	for _, rel := range []string{"submissions/ID.batch", "submissions/ID.unexpected", "evidence/ID/responses/bad", "results/KEY/ID-000000.json"} {
		t.Run(rel, func(t *testing.T) {
			s, _, _, _ := submitFixture(t)
			s.State = "preparing"
			s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			root := t.TempDir()
			r := &Runner{IntentDir: root}
			if err := r.writeIntent(s); err != nil {
				t.Fatal(err)
			}
			guard, err := r.lockOpenAICreator(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = guard.close() }()
			path := filepath.Join(root, strings.ReplaceAll(strings.ReplaceAll(rel, "ID", s.ID), "KEY", s.Items[0].RequestHash))
			if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
			sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
			if err = sink.Check(s); err == nil {
				t.Fatal("accepted earlier evidence")
			}
		})
	}
}

func TestOpenAISubmitDiskRejectsNoncanonicalIntent(t *testing.T) {
	s, _, _, _ := submitFixture(t)
	s.State = "preparing"
	s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	root := t.TempDir()
	r := &Runner{IntentDir: root}
	if err := r.writeIntent(s); err != nil {
		t.Fatal(err)
	}
	guard, err := r.lockOpenAICreator(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.close() }()
	path := filepath.Join(root, "submissions", s.ID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
	if err = sink.Check(s); err == nil {
		t.Fatal("accepted noncanonical intent")
	}
}

func TestOpenAISubmitDiskFailedWritePoisonsSink(t *testing.T) {
	s, _, _, _ := submitFixture(t)
	s.State = "preparing"
	s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	root := t.TempDir()
	r := &Runner{IntentDir: root}
	if err := r.writeIntent(s); err != nil {
		t.Fatal(err)
	}
	guard, err := r.lockOpenAICreator(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.close() }()
	sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
	if err = sink.Check(s); err != nil {
		t.Fatal(err)
	}
	sink.syncDir = func(fd int) error {
		var st unix.Stat_t
		if e := unix.Fstat(fd, &st); e != nil {
			return e
		}
		path := filepath.Join(root, "evidence", s.ID, "openai-submit", "input.json")
		if _, e := os.Stat(path); e == nil {
			return errors.New("sync failed")
		}
		return unix.Fsync(fd)
	}
	if err = sink.Input(s, strings.Repeat("a", 64), []string{customID(s.ID, 0), customID(s.ID, 2)}); err == nil {
		t.Fatal("sync succeeded")
	}
	sink.syncDir = nil
	if err = sink.UploadResponse(s, openaibatch.Response{Status: 200}); err == nil {
		t.Fatal("advanced after failed durability")
	}
}

func TestOpenAISubmitDiskSyncsSubmissions(t *testing.T) {
	s, _, _, _ := submitFixture(t)
	s.State = "preparing"
	s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	root := t.TempDir()
	r := &Runner{IntentDir: root}
	if err := r.writeIntent(s); err != nil {
		t.Fatal(err)
	}
	guard, err := r.lockOpenAICreator(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.close() }()
	expected, err := os.Stat(filepath.Join(root, "submissions"))
	if err != nil {
		t.Fatal(err)
	}
	sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }, syncDir: func(fd int) error {
		var st unix.Stat_t
		if e := unix.Fstat(fd, &st); e != nil {
			return e
		}
		target := expected.Sys().(*syscall.Stat_t)
		if st.Ino == target.Ino && st.Dev == target.Dev {
			return errors.New("submissions sync failed")
		}
		return unix.Fsync(fd)
	}}
	if err = sink.Check(s); err == nil {
		t.Fatal("ignored submissions sync failure")
	}
}

func TestOpenAISubmitDiskRechecksCrossPathOnAdvance(t *testing.T) {
	s, _, _, _ := submitFixture(t)
	s.State = "preparing"
	s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	root := t.TempDir()
	r := &Runner{IntentDir: root}
	if err := r.writeIntent(s); err != nil {
		t.Fatal(err)
	}
	guard, err := r.lockOpenAICreator(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.close() }()
	sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
	if err = sink.Check(s); err != nil {
		t.Fatal(err)
	}
	if err = sink.Input(s, strings.Repeat("a", 64), []string{customID(s.ID, 0), customID(s.ID, 2)}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "submissions", s.ID+".creator.finished"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = sink.UploadResponse(s, openaibatch.Response{Status: 200}); err == nil {
		t.Fatal("accepted earlier evidence on advance")
	}
}

func TestOpenAISubmitDiskRequiresCheck(t *testing.T) {
	s, _, _, _ := submitFixture(t)
	s.State = "preparing"
	s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	sink := &openAISubmitDisk{}
	if err := sink.Input(s, strings.Repeat("a", 64), []string{customID(s.ID, 0), customID(s.ID, 2)}); err == nil {
		t.Fatal("advanced without check")
	}
}

func TestOpenAISubmitDiskBindsCheckedIntent(t *testing.T) {
	s, _, _, _ := submitFixture(t)
	s.State = "preparing"
	s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	root := t.TempDir()
	r := &Runner{IntentDir: root}
	if err := r.writeIntent(s); err != nil {
		t.Fatal(err)
	}
	guard, err := r.lockOpenAICreator(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.close() }()
	sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
	if err = sink.Check(s); err != nil {
		t.Fatal(err)
	}
	s.ProjectID = "proj-other"
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "submissions", s.ID+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = sink.Input(s, strings.Repeat("a", 64), []string{customID(s.ID, 0), customID(s.ID, 2)}); err == nil {
		t.Fatal("accepted different checked scope")
	}
}
