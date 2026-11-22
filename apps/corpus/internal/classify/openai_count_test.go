package classify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/openaibatch"
	"golang.org/x/sys/unix"
)

type countFake struct {
	call  func() (openaibatch.Response, error)
	calls int
}

func (f *countFake) Count(context.Context, []byte) (openaibatch.Response, error) {
	f.calls++
	return f.call()
}

func TestOpenAICountJournal(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{"model":"gpt-6.2-sol"}`), CountBody: []byte(`{"model":"gpt-6.2-sol"}`)}
	f := &countFake{call: func() (openaibatch.Response, error) {
		entries, err := os.ReadDir(filepath.Join(r.IntentDir, "counts", "openai-calls"))
		if err != nil || len(entries) != 2 || entries[0].Name() != ".staging" || !strings.HasSuffix(entries[1].Name(), ".dispatch.json") {
			t.Fatalf("dispatch not durable before call: %v %v", entries, err)
		}
		return openaibatch.Response{Status: 200, Body: []byte(`{"object":"response.input_tokens","input_tokens":272001}`)}, nil
	}}
	n, err := r.openAICountRequest(context.Background(), req, f)
	if err != nil || n != 272001 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	if _, err = r.verifyStore(nil, nil); err != nil {
		t.Fatal(err)
	}
	est, err := r.openAICountEstimate()
	if err != nil || est.MicroUSD != 1088004 || est.Unknown != 0 {
		t.Fatalf("estimate=%+v err=%v", est, err)
	}
	n, err = r.openAICountRequest(context.Background(), req, f)
	if err != nil || n != 272001 || f.calls != 1 {
		t.Fatalf("cache count=%d calls=%d err=%v", n, f.calls, err)
	}
	files, err := os.ReadDir(filepath.Join(r.IntentDir, "counts", "openai-calls"))
	if err != nil || len(files) != 3 {
		t.Fatalf("files=%v err=%v", files, err)
	}
}

func TestOpenAICountUnknown(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
	f := &countFake{call: func() (openaibatch.Response, error) {
		return openaibatch.Response{Status: 200, Body: []byte(`{"input_tokens":20}`)}, errors.New("private message")
	}}
	_, err := r.openAICountRequest(context.Background(), req, f)
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("err=%v", err)
	}
	est, err := r.openAICountEstimate()
	if err != nil || est.Unknown != 1 || est.MicroUSD != 0 {
		t.Fatalf("estimate=%+v err=%v", est, err)
	}
	if _, err = r.verifyStore(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIJournalVerification(t *testing.T) {
	id := strings.Repeat("b", 32)
	key := strings.Repeat("c", 64)
	dispatch := `{"id":"` + id + `","key":"` + key + `","project":"proj-test","fingerprint":"` + strings.Repeat("a", 32) + `","model":"gpt-6.2-sol","version":"` + openAIBatchPriceVersion + `"}`
	result := `{"id":"` + id + `","key":"` + key + `","input_tokens":100,"unknown":false}`
	cases := []struct {
		name             string
		dispatch, result string
		directory        string
		valid            bool
		unknown          int64
	}{
		{name: "interrupted", dispatch: dispatch, valid: true, unknown: 1},
		{name: "completed", dispatch: dispatch, result: result, valid: true},
		{name: "orphan", result: result},
		{name: "malformed", dispatch: dispatch, result: `{`},
		{name: "mismatched", dispatch: dispatch, result: strings.Replace(result, key, strings.Repeat("d", 64), 1)},
		{name: "missing unknown", dispatch: dispatch, result: strings.Replace(result, `,"unknown":false`, "", 1)},
		{name: "unexpected directory", directory: "unexpected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Runner{IntentDir: t.TempDir()}
			dir := filepath.Join(r.IntentDir, "counts", "openai-calls")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if tc.directory != "" {
				if err := os.Mkdir(filepath.Join(r.IntentDir, "counts", tc.directory), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if tc.dispatch != "" {
				if err := os.WriteFile(filepath.Join(dir, id+".dispatch.json"), []byte(tc.dispatch), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.result != "" {
				if err := os.WriteFile(filepath.Join(dir, id+".result.json"), []byte(tc.result), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := r.verifyStore(nil, nil)
			if (err == nil) != tc.valid {
				t.Fatalf("verify error=%v", err)
			}
			if tc.valid {
				est, err := r.openAICountEstimate()
				if err != nil || est.Unknown != tc.unknown {
					t.Fatalf("estimate=%+v err=%v", est, err)
				}
			}
		})
	}
}

func TestOpenAICountCacheCollisionRetainsResult(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
	f := &countFake{call: func() (openaibatch.Response, error) {
		if err := r.writeCount(openAICountKey(req.BatchBody), 99); err != nil {
			t.Fatal(err)
		}
		return openaibatch.Response{Status: 200, Body: []byte(`{"object":"response.input_tokens","input_tokens":100}`)}, nil
	}}
	if _, err := r.openAICountRequest(context.Background(), req, f); err == nil {
		t.Fatal("expected collision")
	}
	est, err := r.openAICountEstimate()
	if err != nil || est.MicroUSD != 200 || est.Unknown != 0 {
		t.Fatalf("estimate=%+v err=%v", est, err)
	}
}

func TestOpenAICountEstimateNoJournal(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	est, err := r.openAICountEstimate()
	if err != nil || est.MicroUSD != 0 || est.Unknown != 0 {
		t.Fatalf("estimate=%+v err=%v", est, err)
	}
}

func TestOpenAICountRejectsMalformedResponses(t *testing.T) {
	for _, body := range []string{`{"input_tokens":0}`, `{"Input_Tokens":1}`, `{"input_tokens":1,"input_tokens":2}`, `{"input_tokens":1,"error":{"message":"secret"}}`, `{"input_tokens":1`} {
		t.Run(body, func(t *testing.T) {
			r := &Runner{IntentDir: t.TempDir()}
			req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
			f := &countFake{call: func() (openaibatch.Response, error) {
				return openaibatch.Response{Status: 200, Body: []byte(body)}, nil
			}}
			if _, err := r.openAICountRequest(context.Background(), req, f); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("err=%v", err)
			}
			est, err := r.openAICountEstimate()
			if err != nil || est.Unknown != 1 {
				t.Fatalf("estimate=%+v err=%v", est, err)
			}
		})
	}
}

func TestOpenAICountKeyAndIdentity(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{"x":1}`), CountBody: []byte(`{}`)}
	f := &countFake{call: func() (openaibatch.Response, error) {
		return openaibatch.Response{Status: 200, Body: []byte(`{"object":"response.input_tokens","input_tokens":100}`)}, nil
	}}
	if _, err := r.openAICountRequest(context.Background(), req, f); err != nil {
		t.Fatal(err)
	}
	req.BatchBody = []byte(`{"x":2}`)
	if _, err := r.openAICountRequest(context.Background(), req, f); err != nil || f.calls != 2 {
		t.Fatalf("calls=%d err=%v", f.calls, err)
	}
	req.Fingerprint = "wrong"
	if _, err := r.openAICountRequest(context.Background(), req, f); err == nil || f.calls != 2 {
		t.Fatalf("calls=%d err=%v", f.calls, err)
	}
}

func TestOpenAICountEstimateOverflow(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	req := openAICountRequest{Model: "gpt-6.2-astra", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
	f := &countFake{call: func() (openaibatch.Response, error) {
		return openaibatch.Response{Status: 200, Body: []byte(`{"object":"response.input_tokens","input_tokens":9223372036854775807}`)}, nil
	}}
	if _, err := r.openAICountRequest(context.Background(), req, f); err != nil {
		t.Fatal(err)
	}
	if _, err := r.openAICountEstimate(); err == nil {
		t.Fatal("expected overflow")
	}
}

func TestOpenAICountEstimateRejectsJournalSymlink(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	target := t.TempDir()
	counts := filepath.Join(r.IntentDir, "counts")
	if err := os.Mkdir(counts, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(counts, "openai-calls")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.openAICountEstimate(); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestOpenAIJournalRecoveryWithoutCache(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
	f := &countFake{call: func() (openaibatch.Response, error) {
		return openaibatch.Response{Status: 200, Body: []byte(`{"object":"response.input_tokens","input_tokens":100}`)}, nil
	}}
	if _, err := r.openAICountRequest(context.Background(), req, f); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(r.countPath(openAICountKey(req.BatchBody))); err != nil {
		t.Fatal(err)
	}
	if n, err := r.openAICountRequest(context.Background(), req, f); err != nil || n != 100 || f.calls != 1 {
		t.Fatalf("n=%d calls=%d err=%v", n, f.calls, err)
	}
}
func TestOpenAICountRejectsWrongDiscriminator(t *testing.T) {
	for _, body := range []string{`{"input_tokens":2}`, `{"object":"wrong","input_tokens":2}`} {
		t.Run(body, func(t *testing.T) {
			r := &Runner{IntentDir: t.TempDir()}
			req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
			f := &countFake{call: func() (openaibatch.Response, error) {
				return openaibatch.Response{Status: 200, Body: []byte(body)}, nil
			}}
			if _, err := r.openAICountRequest(context.Background(), req, f); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
func TestOpenAICountMissingRootEstimate(t *testing.T) {
	r := &Runner{IntentDir: filepath.Join(t.TempDir(), "absent")}
	if e, err := r.openAICountEstimate(); err != nil || e.Unknown != 0 || e.MicroUSD != 0 {
		t.Fatalf("estimate=%+v err=%v", e, err)
	}
}
func TestOpenAICountNoSymlinkWrites(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(r.IntentDir, "counts")); err != nil {
		t.Fatal(err)
	}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
	f := &countFake{call: func() (openaibatch.Response, error) { t.Fatal("unexpected call"); return openaibatch.Response{}, nil }}
	if _, err := r.openAICountRequest(context.Background(), req, f); err == nil {
		t.Fatal("expected error")
	}
	files, err := os.ReadDir(outside)
	if err != nil || len(files) != 0 {
		t.Fatalf("outside files=%v err=%v", files, err)
	}
}

func TestOpenAICountConflictingCallsRemainBlocked(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
	f := &countFake{call: func() (openaibatch.Response, error) {
		return openaibatch.Response{Status: 200, Body: []byte(`{"object":"response.input_tokens","input_tokens":100}`)}, nil
	}}
	if _, err := r.openAICountRequest(context.Background(), req, f); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("b", 32)
	key := openAICountKey(req.BatchBody)
	dir := filepath.Join(r.IntentDir, "counts", "openai-calls")
	d := openAICountDispatch{ID: id, Key: key, Project: req.Project, Fingerprint: req.Fingerprint, Model: req.Model, Version: req.Version}
	result := openAICountResult{ID: id, Key: key, InputTokens: 101}
	for name, v := range map[string]any{id + ".dispatch.json": d, id + ".result.json": result} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if _, err := r.openAICountRequest(context.Background(), req, f); err == nil || f.calls != 1 {
			t.Fatalf("calls=%d err=%v", f.calls, err)
		}
	}
	est, err := r.openAICountEstimate()
	if err != nil || est.MicroUSD != 402 || est.Unknown != 0 {
		t.Fatalf("estimate=%+v err=%v", est, err)
	}
}

func TestOpenAICountStagingInterruption(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
	f := &countFake{call: func() (openaibatch.Response, error) { panic("interrupted") }}
	func() { defer func() { _ = recover() }(); _, _ = r.openAICountRequest(context.Background(), req, f) }()
	staging := filepath.Join(r.IntentDir, "counts", "openai-calls", ".staging")
	if err := os.WriteFile(filepath.Join(staging, "partial"), []byte(`{`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.verifyStore(nil, nil); err != nil {
		t.Fatal(err)
	}
	est, err := r.openAICountEstimate()
	if err != nil || est.Unknown != 1 {
		t.Fatalf("estimate=%+v err=%v", est, err)
	}
}

func TestOpenAICountRejectsSymlinkedEvidence(t *testing.T) {
	for _, target := range []string{"journal", "cache", "record", "staging"} {
		t.Run(target, func(t *testing.T) {
			r := &Runner{IntentDir: t.TempDir()}
			outside := t.TempDir()
			counts := filepath.Join(r.IntentDir, "counts")
			if err := os.Mkdir(counts, 0700); err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(counts, "openai-calls")
			if target == "journal" {
				if err := os.Symlink(outside, journal); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(journal, 0700); err != nil {
					t.Fatal(err)
				}
				var path string
				switch target {
				case "cache":
					path = filepath.Join(counts, strings.Repeat("a", 64)+".json")
				case "record":
					path = filepath.Join(journal, strings.Repeat("b", 32)+".dispatch.json")
				case "staging":
					path = filepath.Join(journal, ".staging")
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.verifyStore(nil, nil); err == nil {
				t.Fatal("verify accepted symlink")
			}
			if _, err := r.openAICountEstimate(); (err == nil) != (target == "cache") {
				t.Fatalf("estimate error=%v", err)
			}
		})
	}
}

func TestOpenAICountRetainsResultDespiteCorruptCache(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
	f := &countFake{call: func() (openaibatch.Response, error) {
		if err := os.WriteFile(filepath.Join(r.IntentDir, "counts", strings.Repeat("d", 64)+".json"), []byte(`{`), 0600); err != nil {
			t.Fatal(err)
		}
		return openaibatch.Response{Status: 200, Body: []byte(`{"object":"response.input_tokens","input_tokens":100}`)}, nil
	}}
	if _, err := r.openAICountRequest(context.Background(), req, f); err == nil {
		t.Fatal("expected corrupt cache error")
	}
	est, err := r.openAICountEstimate()
	if err != nil || est.MicroUSD != 200 || est.Unknown != 0 {
		t.Fatalf("estimate=%+v err=%v", est, err)
	}
}
func TestOpenAICountRequiresOriginalDispatch(t *testing.T) {
	for _, mode := range []string{"removed", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			r := &Runner{IntentDir: t.TempDir()}
			req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
			f := &countFake{call: func() (openaibatch.Response, error) {
				dir := filepath.Join(r.IntentDir, "counts", "openai-calls")
				files, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				for _, file := range files {
					if strings.HasSuffix(file.Name(), ".dispatch.json") {
						path := filepath.Join(dir, file.Name())
						if mode == "removed" {
							err = os.Remove(path)
						} else {
							err = os.WriteFile(path, []byte(`{}`), 0600)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				return openaibatch.Response{Status: 200, Body: []byte(`{"object":"response.input_tokens","input_tokens":100}`)}, nil
			}}
			if _, err := r.openAICountRequest(context.Background(), req, f); err == nil {
				t.Fatal("expected dispatch error")
			}
			files, err := os.ReadDir(filepath.Join(r.IntentDir, "counts", "openai-calls"))
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if strings.HasSuffix(file.Name(), ".result.json") {
					t.Fatal("orphan result")
				}
			}
			if _, err := os.Stat(r.countPath(openAICountKey(req.BatchBody))); !os.IsNotExist(err) {
				t.Fatalf("cache stat: %v", err)
			}
		})
	}
}
func TestOpenAICountRecoveryBarrier(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
	f := &countFake{call: func() (openaibatch.Response, error) {
		return openaibatch.Response{Status: 200, Body: []byte(`{"object":"response.input_tokens","input_tokens":100}`)}, nil
	}}
	if _, err := r.openAICountRequest(context.Background(), req, f); err != nil {
		t.Fatal(err)
	}
	fail := func(int) error { return errors.New("sync failed") }
	if _, err := r.openAICountRequestWithSync(context.Background(), req, f, fail); err == nil || f.calls != 1 {
		t.Fatalf("calls=%d err=%v", f.calls, err)
	}
	if _, err := r.openAICountEstimateWithSync(fail); err == nil {
		t.Fatal("estimate skipped barrier")
	}
	if n, err := r.openAICountRequest(context.Background(), req, f); err != nil || n != 100 || f.calls != 1 {
		t.Fatalf("n=%d calls=%d err=%v", n, f.calls, err)
	}
}

func TestOpenAICountResultLinkSyncFailureRecovery(t *testing.T) {
	r := &Runner{IntentDir: t.TempDir()}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
	afterCall := false
	f := &countFake{call: func() (openaibatch.Response, error) {
		afterCall = true
		return openaibatch.Response{Status: 200, Body: []byte(`{"object":"response.input_tokens","input_tokens":100}`)}, nil
	}}
	syncDir := func(fd int) error {
		if afterCall {
			return errors.New("sync failed")
		}
		return unix.Fsync(fd)
	}
	if _, err := r.openAICountRequestWithSync(context.Background(), req, f, syncDir); err == nil || f.calls != 1 {
		t.Fatalf("calls=%d err=%v", f.calls, err)
	}
	if n, err := r.openAICountRequest(context.Background(), req, f); err != nil || n != 100 || f.calls != 1 {
		t.Fatalf("n=%d calls=%d err=%v", n, f.calls, err)
	}
}

func TestOpenAICountRequiresExistingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	r := &Runner{IntentDir: root}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
	f := &countFake{call: func() (openaibatch.Response, error) { return openaibatch.Response{}, nil }}
	if _, err := r.openAICountRequest(context.Background(), req, f); err == nil || f.calls != 0 {
		t.Fatalf("calls=%d err=%v", f.calls, err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("root exists: %v", err)
	}
}
func TestOpenAICountParentBarrier(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	r := &Runner{IntentDir: root}
	req := openAICountRequest{Model: "gpt-6.2-sol", Version: openAIBatchPriceVersion, Project: "proj-test", Fingerprint: strings.Repeat("a", 32), BatchBody: []byte(`{}`), CountBody: []byte(`{}`)}
	f := &countFake{call: func() (openaibatch.Response, error) {
		return openaibatch.Response{Status: 200, Body: []byte(`{"object":"response.input_tokens","input_tokens":100}`)}, nil
	}}
	var parentStat unix.Stat_t
	if err := unix.Stat(parent, &parentStat); err != nil {
		t.Fatal(err)
	}
	failParent := func(fd int) error {
		var st unix.Stat_t
		if err := unix.Fstat(fd, &st); err != nil {
			return err
		}
		if st.Dev == parentStat.Dev && st.Ino == parentStat.Ino {
			return errors.New("parent sync failed")
		}
		return unix.Fsync(fd)
	}
	if _, err := r.openAICountRequestWithSync(context.Background(), req, f, failParent); err == nil || f.calls != 0 {
		t.Fatalf("calls=%d err=%v", f.calls, err)
	}
	if n, err := r.openAICountRequest(context.Background(), req, f); err != nil || n != 100 || f.calls != 1 {
		t.Fatalf("n=%d calls=%d err=%v", n, f.calls, err)
	}
	if _, err := r.openAICountRequestWithSync(context.Background(), req, f, failParent); err == nil || f.calls != 1 {
		t.Fatalf("recovery calls=%d err=%v", f.calls, err)
	}
	if n, err := r.openAICountRequest(context.Background(), req, f); err != nil || n != 100 || f.calls != 1 {
		t.Fatalf("retry n=%d calls=%d err=%v", n, f.calls, err)
	}
}
