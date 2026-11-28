//go:build darwin

package pdftext

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type cancelAfterCalls struct {
	context.Context
	remaining int
}

func (c *cancelAfterCalls) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func TestRuntimeSetup_CancelTraversal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	poppler := filepath.Join(root, "poppler", "1")
	if err := os.MkdirAll(filepath.Join(poppler, "share", "poppler"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(poppler, "share", "poppler", name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"data", func(ctx context.Context) error { _, err := runtimeDataPathsContext(ctx, root, poppler); return err }},
		{"cache", func(ctx context.Context) error { _, err := cacheIdentitiesContext(ctx, []string{root}); return err }},
		{"inventory", func(ctx context.Context) error {
			_, err := snapshotRuntimeContext(ctx, runtimeSet{dataDirs: []string{root}})
			return err
		}},
		{"identity", func(ctx context.Context) error {
			_, err := runtimeIdentityContext(ctx, runtimeSet{files: []string{filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "c")}}, "version")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remaining := 5
			if tc.name == "identity" {
				remaining = 10
			}
			ctx := &cancelAfterCalls{Context: context.Background(), remaining: remaining}
			if err := tc.run(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("traversal cancellation: %v", err)
			}
		})
	}
}

func TestVerifyRuntime_CancelTraversal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	set := runtimeSet{dataDirs: []string{root}}
	var err error
	set.snapshot, err = snapshotRuntime(set)
	if err != nil {
		t.Fatal(err)
	}
	set.identity = "invented"
	e := &Extractor{runtime: set, platform: func(context.Context, []string) (platformIdentity, error) {
		t.Fatal("platform reached after cancellation")
		return platformIdentity{}, nil
	}}
	ctx := &cancelAfterCalls{Context: context.Background(), remaining: 4}
	if err := e.VerifyRuntime(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("verification cancellation: %v", err)
	}
}

func TestVerifyRuntime_CancelPlatform(t *testing.T) {
	set := runtimeSet{dataDirs: []string{t.TempDir()}}
	var err error
	set.snapshot, err = snapshotRuntime(set)
	if err != nil {
		t.Fatal(err)
	}
	set.identity = "invented"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := false
	e := &Extractor{runtime: set, platform: func(got context.Context, _ []string) (platformIdentity, error) {
		called = true
		if got != ctx {
			t.Fatal("platform received a different context")
		}
		cancel()
		return platformIdentity{}, got.Err()
	}}
	if err := e.VerifyRuntime(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("platform cancellation: %v", err)
	}
	if !called {
		t.Fatal("platform was not reached")
	}
}

func TestProcess_CancelAfterReapSkipsRuntimeWalk(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start(context.Background(), []string{"-c", `while :; do :; done`}, nil, 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	p.runtime.snapshot = append(p.runtime.snapshot, runtimeEntry{path: "invented mismatch"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = p.wait(ctx)
	if !errors.Is(err, ErrFile) {
		t.Fatalf("cancellation: %v", err)
	}
	if err = syscall.Kill(-p.cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process group remains: %v", err)
	}
}
