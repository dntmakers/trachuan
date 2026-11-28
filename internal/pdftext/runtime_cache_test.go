package pdftext

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheIdentities_RejectsMalformedCaches(t *testing.T) {
	root := t.TempDir()
	if _, err := cacheIdentities([]string{root}); err == nil {
		t.Fatal("empty cache accepted")
	}
	header := make([]byte, 104)
	copy(header, "dyld_v1 arm64")
	copy(header[88:], []byte("0123456789abcdef"))
	cache := filepath.Join(root, "dyld_shared_cache_arm64")
	if writeErr := os.WriteFile(cache, header, 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if writeErr := os.WriteFile(cache+".map", []byte("map"), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if writeErr := os.WriteFile(cache+".atlas", []byte("atlas"), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	got, err := cacheIdentities([]string{root})
	if err != nil || len(got) != 1 {
		t.Fatalf("cache list: %v, %v", got, err)
	}
	if writeErr := os.WriteFile(cache, []byte("bad"), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if _, err := cacheIdentities([]string{root}); err == nil {
		t.Fatal("truncated cache accepted")
	}
}

func TestReadCacheUUID(t *testing.T) {
	header := make([]byte, 104)
	copy(header, "dyld_v1 arm64")
	copy(header[88:], []byte("0123456789abcdef"))
	path := filepath.Join(t.TempDir(), "cache")
	if writeErr := os.WriteFile(path, header, 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	uuid, err := readCacheUUID(path)
	if err != nil || !bytes.Equal(uuid[:], []byte("0123456789abcdef")) {
		t.Fatalf("UUID: %x, %v", uuid, err)
	}
	for _, invalid := range [][]byte{header[:103], append([]byte("notdyld!"), header[8:]...), make([]byte, 104)} {
		if writeErr := os.WriteFile(path, invalid, 0600); writeErr != nil {
			t.Fatal(writeErr)
		}
		if _, err := readCacheUUID(path); err == nil {
			t.Fatal("invalid cache accepted")
		}
	}
}
