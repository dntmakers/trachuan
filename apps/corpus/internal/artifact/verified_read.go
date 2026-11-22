package artifact

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ReadVerifiedFile(dir, name, manifestHash string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("invalid artifact directory")
	}
	manifestPath := filepath.Join(dir, manifestName)
	info, err = os.Lstat(manifestPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid artifact manifest")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != manifestHash {
		return nil, fmt.Errorf("manifest hash mismatch")
	}
	entries, err := parseManifest(data)
	if err != nil {
		return nil, err
	}
	if !filepath.IsLocal(name) || name == "." || name == manifestName || strings.ContainsAny(name, "\n\r") || maxBytes < 0 {
		return nil, fmt.Errorf("invalid artifact path or limit")
	}
	expected, ok := entries[filepath.ToSlash(name)]
	if !ok {
		return nil, fmt.Errorf("manifest missing %q", name)
	}
	path := dir
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	for i, part := range parts {
		path = filepath.Join(path, part)
		info, err = os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", name, err)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, fmt.Errorf("artifact parent is not a directory: %s", name)
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("artifact is not a regular file: %s", name)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(f, maxBytes+1))
	closeErr := f.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("artifact file exceeds limit: %s", name)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != expected {
		return nil, fmt.Errorf("manifest mismatch for %s", name)
	}
	return data, nil
}
