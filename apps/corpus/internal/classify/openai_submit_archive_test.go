package classify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenAISubmitArchivePrefixes(t *testing.T) {
	for n := 0; n <= len(submitNames); n++ {
		t.Run(string(rune('0'+n)), func(t *testing.T) {
			s, bodies, client, _ := submitFixture(t)
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
			sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
			if _, err = openAISubmit(context.Background(), s, bodies, client, sink); err != nil {
				t.Fatal(err)
			}
			if err = guard.close(); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "evidence", s.ID, "openai-submit")
			for _, name := range submitNames[n:] {
				if err = os.Remove(filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			got, err := readOpenAISubmitArchive(root, s, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := archiveNoDispatch
			if n >= 4 {
				want = archiveDispatchUncertain
			}
			if n == 6 {
				want = archiveVerifiedReceipt
			}
			if got.Phase != want {
				t.Fatalf("phase %v want %v", got.Phase, want)
			}
			if n >= 2 && (got.UploadStatus != client.upload.Status || string(got.UploadBody) != string(client.upload.Body)) {
				t.Fatal("upload response not recovered")
			}
			if n >= 5 && (got.ResponseStatus != client.create.Status || string(got.ResponseBody) != string(client.create.Body)) {
				t.Fatal("create response not recovered")
			}
			if n == 6 && got.BatchID != "batch-test" {
				t.Fatalf("batch %q", got.BatchID)
			}
		})
	}
}

func TestOpenAISubmitArchiveRejectsCorruption(t *testing.T) {
	for _, mode := range []string{"scope", "hash", "missing", "extra", "symlink", "sync", "project", "field", "time", "zero-field"} {
		t.Run(mode, func(t *testing.T) {
			s, bodies, client, _ := submitFixture(t)
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
			sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time { return s.CreatedAt }}
			if _, err = openAISubmit(context.Background(), s, bodies, client, sink); err != nil {
				t.Fatal(err)
			}
			if err = guard.close(); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "evidence", s.ID, "openai-submit")
			path := filepath.Join(dir, "batch-receipt.json")
			var syncDir func(int) error
			switch mode {
			case "missing":
				err = os.Remove(filepath.Join(dir, "upload-receipt.json"))
			case "extra":
				err = os.WriteFile(filepath.Join(dir, "unknown"), nil, 0600)
			case "symlink":
				err = os.Remove(path)
				if err == nil {
					err = os.Symlink("input.json", path)
				}
			case "sync":
				syncDir = func(int) error { return errors.New("sync") }
			case "zero-field":
				raw, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				raw = append(raw[:len(raw)-1], []byte(`,"ids":null}`)...)
				err = os.WriteFile(path, raw, 0600)
			case "project":
				s.ProjectID = "proj-other"
			default:
				raw, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				var record submitRecord
				if e = json.Unmarshal(raw, &record); e != nil {
					t.Fatal(e)
				}
				switch mode {
				case "scope":
					record.Project = "proj-other"
				case "hash":
					record.ResponseHash = strings.Repeat("a", 64)
				case "field":
					record.Status = 201
				case "time":
					record.At = s.CreatedAt.Add(-time.Second)
				}
				raw, e = json.Marshal(record)
				if e != nil {
					t.Fatal(e)
				}
				err = os.WriteFile(path, raw, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, e := readOpenAISubmitArchive(root, s, syncDir); e == nil {
				t.Fatalf("accepted %s: %+v", mode, got)
			}
		})
	}
}

func TestOpenAISubmitArchiveBlockedStoreUnchanged(t *testing.T) {
	s, _, _, _ := submitFixture(t)
	s.State = "preparing"
	s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "submissions"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readOpenAISubmitArchive(root, s, nil); err == nil {
		t.Fatal("accepted blocked store")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "submissions" {
		t.Fatalf("store changed: %v", entries)
	}
	raw, err := os.ReadFile(filepath.Join(root, "submissions"))
	if err != nil || string(raw) != "blocked" {
		t.Fatalf("blocked entry changed: %v", err)
	}
}

func TestOpenAISubmitArchiveClockRollback(t *testing.T) {
	for _, start := range []time.Duration{-time.Second, time.Second} {
		t.Run(start.String(), func(t *testing.T) {
			s, bodies, client, _ := submitFixture(t)
			s.State = "preparing"
			s.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			root := t.TempDir()
			runner := &Runner{IntentDir: root}
			if err := runner.writeIntent(s); err != nil {
				t.Fatal(err)
			}
			guard, err := runner.lockOpenAICreator(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = guard.close() }()
			step := 0
			sink := &openAISubmitDisk{root: root, guard: guard, now: func() time.Time {
				step++
				if step == 1 {
					return s.CreatedAt.Add(start)
				}
				if step == 2 {
					return s.CreatedAt.Add(10 * time.Second)
				}
				return s.CreatedAt.Add(time.Second)
			}}
			if _, err = openAISubmit(context.Background(), s, bodies, client, sink); err != nil {
				t.Fatal(err)
			}
			result, err := readOpenAISubmitArchive(root, s, nil)
			if err != nil || result.Phase != archiveVerifiedReceipt {
				t.Fatalf("phase=%v err=%v", result.Phase, err)
			}
			for i, name := range submitNames {
				raw, e := os.ReadFile(filepath.Join(root, "evidence", s.ID, "openai-submit", name))
				if e != nil {
					t.Fatal(e)
				}
				var record submitRecord
				if e = json.Unmarshal(raw, &record); e != nil {
					t.Fatal(e)
				}
				want := s.CreatedAt
				if i == 0 && start > 0 {
					want = want.Add(start)
				}
				if i > 0 {
					want = want.Add(10 * time.Second)
				}
				if !record.At.Equal(want) {
					t.Fatalf("%s time %v want %v", name, record.At, want)
				}
			}
		})
	}
}
