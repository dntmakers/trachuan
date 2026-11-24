package embed

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
)

func TestDecodeFile_TrailingContentIsAnArtifactError(t *testing.T) {
	for _, input := range []string{"{} {}", "{} ]", "{} {", "{} 1e999999"} {
		t.Run(input, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(input), 0600); err != nil {
				t.Fatal(err)
			}
			var dst struct{}
			err := decodeFile(dir, "index.json", &dst)
			var syntax *json.SyntaxError
			var typeErr *json.UnmarshalTypeError
			if err == nil || errors.As(err, &syntax) || errors.As(err, &typeErr) || errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("decode file error = %v, want artifact-only error", err)
			}
		})
	}
}

func TestDecodeFile_PreservesReadError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "index.json"), 0700); err != nil {
		t.Fatal(err)
	}
	var dst struct{}
	err := decodeFile(dir, "index.json", &dst)
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || !errors.Is(err, syscall.EISDIR) || invalidCachedArtifact(err).stale {
		t.Fatalf("decode file error = %v, want distinguishable read error", err)
	}
}

func TestDecodeArtifact_PreservesTrailingReadError(t *testing.T) {
	readErr := &os.PathError{Op: "read", Path: "index.json", Err: syscall.EIO}
	r := io.MultiReader(strings.NewReader("{} "), iotest.ErrReader(readErr))
	var dst struct{}
	err := decodeArtifact("index.json", r, &dst)
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || !errors.Is(err, syscall.EIO) || invalidCachedArtifact(err).stale {
		t.Fatalf("decode artifact error = %v, want distinguishable trailing read error", err)
	}
}
