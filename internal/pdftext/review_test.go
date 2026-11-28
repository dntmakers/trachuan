//go:build darwin

package pdftext

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRetryInterrupted(t *testing.T) {
	calls := 0
	err := retryInterrupted(func() error {
		calls++
		if calls < 3 {
			return unix.EINTR
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("interrupted wait: %d calls, %v", calls, err)
	}
	if err := retryInterrupted(func() error { return unix.EINVAL }); !errors.Is(err, unix.EINVAL) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractor_RuntimeIdentity(t *testing.T) {
	e, err := NewExtractor(NewLimiter())
	if err != nil {
		t.Fatal(err)
	}
	identity := e.RuntimeIdentity()
	decoded, err := hex.DecodeString(identity)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("invalid runtime identity: %v", err)
	}
}

func TestRuntimeSet(t *testing.T) {
	path, err := filepath.EvalSymlinks("/opt/homebrew/bin/pdftotext")
	if err != nil {
		t.Fatal(err)
	}
	set, err := resolveRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.files) == 0 {
		t.Fatal("no dependencies")
	}
	e := &Extractor{path: path, runtime: set, limiter: NewLimiter()}
	if err := e.checkVersion(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractor_VerifiesCurrentRuntime(t *testing.T) {
	e, err := NewExtractor(NewLimiter())
	if err != nil {
		t.Fatal(err)
	}
	if verifyErr := e.VerifyRuntime(); verifyErr != nil {
		t.Fatal(verifyErr)
	}
}

func TestExtractor_RejectsRuntimeChangeRestoredDuringRun(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cmap")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	set := testRuntime()
	set.dataDirs = []string{root}
	set.snapshot, err = snapshotRuntime(set)
	if err != nil {
		t.Fatal(err)
	}
	e := &Extractor{path: "/bin/bash", runtime: set, limiter: NewLimiter()}
	p, err := e.start([]string{"-c", `read x; printf '%s' "$x"`}, []byte("input\n"), 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	var mutationErr error
	mutated := false
	p.sample = func(context.Context, int) (uint64, error) {
		if !mutated {
			mutated = true
			mutationErr = os.WriteFile(path, []byte("other"), 0600)
			if mutationErr == nil {
				mutationErr = os.WriteFile(path, []byte("first"), 0600)
			}
			if mutationErr == nil {
				mutationErr = os.Chtimes(path, before.ModTime(), before.ModTime())
			}
		}
		return 0, mutationErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err = p.wait(ctx)
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
	after, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("test did not restore the old fingerprint")
	}
	if !errors.Is(err, ErrConfiguration) {
		t.Fatalf("restored runtime change accepted: %v", err)
	}
}

func TestExtractor_RejectsRuntimeMutationBeforeNextRun(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cmap")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	set := testRuntime()
	set.dataDirs = []string{root}
	var err error
	set.snapshot, err = snapshotRuntime(set)
	if err != nil {
		t.Fatal(err)
	}
	e := &Extractor{path: "/bin/cat", runtime: set, limiter: NewLimiter()}
	if _, err := e.run(context.Background(), nil, []byte("first"), time.Second, 100, 1024); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run(context.Background(), nil, []byte("second"), time.Second, 100, 1024); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("changed runtime accepted: %v", err)
	}
}

func TestExtractor_VerifyRuntimeDetectsSameStatContentChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cmap")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	platform := platformIdentity{build: "test-build", caches: []cacheIdentity{{path: "/cache", uuid: [16]byte{1}}}}
	set := runtimeSet{dataDirs: []string{root}, platform: platform}
	var err error
	set.snapshot, err = snapshotRuntime(set)
	if err != nil {
		t.Fatal(err)
	}
	set.identity, err = runtimeIdentity(set, "pdftotext version "+Version+"\n")
	if err != nil {
		t.Fatal(err)
	}
	e := &Extractor{runtime: set, platform: func([]string) (platformIdentity, error) { return platform, nil }}
	if verifyErr := e.VerifyRuntime(); verifyErr != nil {
		t.Fatal(verifyErr)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(path, []byte("other"), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if touchErr := os.Chtimes(path, info.ModTime(), info.ModTime()); touchErr != nil {
		t.Fatal(touchErr)
	}
	if checkErr := checkRuntimeSnapshot(set, set.snapshot); checkErr == nil {
		t.Fatal("ctime change was not detected")
	}
	e.runtime.snapshot, err = snapshotRuntime(set)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.VerifyRuntime(); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("changed content accepted: %v", err)
	}
}

func TestSandbox_DeniesWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	if _, err := e.run(context.Background(), []string{"-c", "exit 0"}, nil, time.Second, 100, 1024); err != nil {
		t.Fatalf("shell failed: %v", err)
	}
	if _, err := e.run(context.Background(), []string{"-c", `printf x > "$1"`, "sandbox-test", path}, nil, time.Second, 100, 1024); !errors.Is(err, ErrFile) {
		t.Fatalf("write was not denied: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output created: %v", err)
	}
}

func TestRun_InputSizeBoundary(t *testing.T) {
	e := &Extractor{path: "/bin/cat", runtime: testRuntime(), limiter: NewLimiter()}
	data := make([]byte, maxInput+1)
	output, err := e.run(context.Background(), nil, data[:maxInput], 10*time.Second, maxInput, 1024)
	if err != nil || len(output) != maxInput {
		t.Fatalf("boundary input: %d, %v", len(output), err)
	}
	if _, err := e.run(context.Background(), nil, data, 10*time.Second, maxInput, 1024); !errors.Is(err, ErrFile) {
		t.Fatalf("oversized input accepted: %v", err)
	}
}

func childCount() (int, error) {
	cmd := exec.Command("/bin/ps", "-axo", "ppid=")
	cmd.Env = []string{"LC_ALL=C"}
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, s := range strings.Fields(string(output)) {
		n, parseErr := strconv.Atoi(s)
		if parseErr == nil && n == os.Getpid() {
			count++
		}
	}
	return count, nil
}

func TestWatchProcess_StaysPending(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start([]string{"-c", `while :; do :; done`}, nil, 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-p.done:
		p.observed = true
		_, _, _ = p.kill(ErrFile)
		t.Fatalf("premature process event: %v", result.err)
	case <-time.After(30 * time.Millisecond):
	}
	_, _, _ = p.kill(ErrFile)
}

func TestLimiter_ThreeRealRuns(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	ctx3, cancel3 := context.WithCancel(context.Background())
	var runs sync.WaitGroup
	defer func() { cancel1(); cancel2(); cancel3(); runs.Wait() }()
	first := make(chan error, 1)
	second := make(chan error, 1)
	third := make(chan error, 1)
	runs.Add(2)
	go func() {
		defer runs.Done()
		_, err := e.run(ctx1, []string{"-c", `while :; do :; done`}, nil, time.Second, 100, 1024)
		first <- err
	}()
	go func() {
		defer runs.Done()
		_, err := e.run(ctx2, []string{"-c", `while :; do :; done`}, nil, time.Second, 100, 1024)
		second <- err
	}()
	deadline := time.Now().Add(90 * time.Millisecond)
	for {
		count, err := childCount()
		if err != nil {
			t.Fatal(err)
		}
		if count >= 3 && len(e.limiter.slots) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("two subprocesses did not start")
		}
	}
	runs.Add(1)
	go func() {
		defer runs.Done()
		_, err := e.run(ctx3, []string{"-c", "exit 0"}, nil, time.Second, 100, 1024)
		third <- err
	}()
	select {
	case err := <-third:
		var firstErr, secondErr error
		select {
		case firstErr = <-first:
		default:
		}
		select {
		case secondErr = <-second:
		default:
		}
		t.Fatalf("third run bypassed limit: %v, first %v, second %v", err, firstErr, secondErr)
	case <-time.After(30 * time.Millisecond):
	}
	cancel1()
	if err := <-first; !errors.Is(err, ErrFile) {
		t.Fatalf("first run: %v", err)
	}
	select {
	case err := <-third:
		if err != nil {
			t.Fatalf("third run: %v", err)
		}
	case <-time.After(90 * time.Millisecond):
		t.Fatal("third run did not start")
	}
	cancel2()
	if err := <-second; !errors.Is(err, ErrFile) {
		t.Fatalf("second run: %v", err)
	}
}

func TestProcess_CancelledEvenWhenExitReady(t *testing.T) {
	for i := 0; i < 10; i++ {
		e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
		p, err := e.start([]string{"-c", "exit 0"}, nil, 1024, 1024)
		if err != nil {
			t.Fatal(err)
		}
		result := <-p.done
		ready := make(chan processResult, 1)
		ready <- result
		p.done = ready
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err = p.wait(ctx)
		if !errors.Is(err, ErrFile) {
			t.Fatalf("cancelled completion accepted: %v", err)
		}
	}
}

func TestProcess_NoSignalAfterReap(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start([]string{"-c", "exit 0"}, nil, 1024, 1024)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = p.wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.signal(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("reaped process signal: %v", err)
	}
}

func TestProcess_RejectsIncompleteInput(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	_, err := e.run(context.Background(), []string{"-c", "exit 0"}, make([]byte, 1_000_000), time.Second, 1024, 1024)
	if !errors.Is(err, ErrFile) {
		t.Fatalf("incomplete input accepted: %v", err)
	}
}

func TestSampler_VerifiedBeforeInput(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start([]string{"-c", `read x; printf '%s' "$x"`}, []byte("publisher-sentinel\n"), 1024, 1024)
	if err != nil {
		t.Fatal(err)
	}
	p.sample = func(context.Context, int) (uint64, error) { return 0, errors.New("unavailable") }
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Millisecond)
	defer cancel()
	_, _, err = p.wait(ctx)
	if !errors.Is(err, ErrConfiguration) {
		t.Fatalf("expected sampler error: %v", err)
	}
	if strings.Contains(p.stdout.buf.String(), "publisher-sentinel") {
		t.Fatal("input reached tool before sampler check")
	}
}

func TestVersionCheck_UsesSharedLimiter(t *testing.T) {
	limit := NewLimiter()
	limit.slots <- struct{}{}
	limit.slots <- struct{}{}
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: limit}
	done := make(chan struct{ err error }, 1)
	go func() { done <- struct{ err error }{e.checkVersion()} }()
	select {
	case <-done:
		t.Fatal("version check bypassed limiter")
	case <-time.After(30 * time.Millisecond):
	}
	<-limit.slots
	select {
	case result := <-done:
		if !errors.Is(result.err, ErrConfiguration) {
			t.Fatalf("version check: %v", result.err)
		}
	case <-time.After(60 * time.Millisecond):
		t.Fatal("version check did not acquire slot")
	}
}

func TestExtractor_SharedLimiter(t *testing.T) {
	limit := NewLimiter()
	first := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: limit}
	second := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: limit}
	limit.slots <- struct{}{}
	limit.slots <- struct{}{}
	for _, e := range []*Extractor{first, second} {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		_, err := e.run(ctx, []string{"-c", "exit 0"}, nil, time.Second, 100, 100)
		cancel()
		if !errors.Is(err, ErrFile) {
			t.Fatalf("limit not shared: %v", err)
		}
	}
}

func TestSandbox_DeniesRuntimeNeighbors(t *testing.T) {
	e, err := NewExtractor(NewLimiter())
	if err != nil {
		t.Fatal(err)
	}
	otherShare, err := filepath.EvalSymlinks("/opt/homebrew/opt/fontconfig/share/fontconfig/conf.avail/10-hinting-full.conf")
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		otherShare,
		filepath.Join(filepath.Dir(filepath.Dir(e.path)), "share", "man", "man1", "pdftotext.1"),
		"/System/Library/CoreServices/Finder.app/Contents/Info.plist",
		"/usr/lib/libgmalloc.dylib",
	}
	cat := &Extractor{path: "/bin/cat", runtime: e.runtime, limiter: NewLimiter()}
	if _, err := cat.run(context.Background(), nil, []byte("ok"), time.Second, 100, 1024); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() > 1_000_000 {
				t.Fatal("fixture unexpectedly large")
			}
			if _, err := cat.run(context.Background(), []string{path}, nil, time.Second, int(info.Size())+1, 1024); !errors.Is(err, ErrFile) {
				t.Fatalf("runtime neighbor readable: %v", err)
			}
		})
	}
}

func TestSandbox_DeniesKegRootAndSystemApps(t *testing.T) {
	e, err := NewExtractor(NewLimiter())
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(filepath.Dir(filepath.Dir(e.path)), "INSTALL_RECEIPT.json"), "/System/Applications/Calculator.app/Contents/Info.plist"}
	cat := &Extractor{path: "/bin/cat", runtime: e.runtime, limiter: NewLimiter()}
	if _, err := cat.run(context.Background(), nil, []byte("ok"), time.Second, 100, 1024); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() > 1_000_000 {
				t.Fatal("fixture unexpectedly large")
			}
			if _, err := cat.run(context.Background(), []string{path}, nil, time.Second, int(info.Size())+1, 1024); !errors.Is(err, ErrFile) {
				t.Fatalf("restricted path readable: %v", err)
			}
		})
	}
}

func TestSandbox_RejectsUnrelatedKeg(t *testing.T) {
	e, err := NewExtractor(NewLimiter())
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("/opt/homebrew/Cellar")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		path := filepath.Join("/opt/homebrew/Cellar", entry.Name())
		allowed := false
		for _, root := range append(append([]string(nil), e.runtime.files...), e.runtime.dataDirs...) {
			if strings.HasPrefix(root, path+"/") {
				allowed = true
				break
			}
		}
		if allowed {
			continue
		}
		versions, err := os.ReadDir(path)
		if err != nil || len(versions) == 0 {
			continue
		}
		keg := filepath.Join(path, versions[0].Name())
		var candidate string
		_ = filepath.WalkDir(keg, func(path string, d os.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				info, infoErr := d.Info()
				if infoErr == nil && info.Mode().Perm()&0444 != 0 && info.Size() > 0 && info.Size() <= 1024 {
					candidate = path
					return filepath.SkipAll
				}
			}
			return nil
		})
		if candidate == "" {
			continue
		}
		cat := &Extractor{path: "/bin/cat", runtime: e.runtime, limiter: NewLimiter()}
		if out, err := cat.run(context.Background(), nil, []byte("ok"), time.Second, 100, 1024); err != nil || string(out) != "ok" {
			t.Fatalf("cat failed: %v", err)
		}
		if _, err := cat.run(context.Background(), []string{candidate}, nil, time.Second, 2048, 1024); err == nil {
			t.Fatal("unrelated keg readable")
		}
		shell := &Extractor{path: "/bin/bash", runtime: e.runtime, limiter: NewLimiter()}
		if _, err := shell.run(context.Background(), []string{"-c", "exit 0"}, nil, time.Second, 100, 1024); err != nil {
			t.Fatalf("shell failed: %v", err)
		}
		if _, err := shell.run(context.Background(), []string{"-c", `[[ -e "$1" ]]`, "sandbox-test", candidate}, nil, time.Second, 100, 1024); err == nil {
			t.Fatal("unrelated keg metadata readable")
		}
		return
	}
	t.Fatal("no unrelated keg found")
}
