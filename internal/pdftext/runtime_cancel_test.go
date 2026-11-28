package pdftext

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
