package pdftext

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

type runtimeSet struct {
	files       []string
	systemFiles []string
	dataDirs    []string
	systemDirs  []string
	cacheDirs   []string
	metadata    []string
	platform    platformIdentity
	snapshot    runtimeSnapshot
	identity    string
}

func runtimeIdentity(set runtimeSet, versionLine string) (string, error) {
	entries, err := snapshotRuntime(set)
	if err != nil {
		return "", err
	}
	caches := append([]cacheIdentity(nil), set.platform.caches...)
	sort.Slice(caches, func(i, j int) bool { return caches[i].path < caches[j].path })
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%d:%s\n%d:%s\n", len(versionLine), versionLine, len(set.platform.build), set.platform.build)
	for i, cache := range caches {
		if cache.path == "" || cache.uuid == [16]byte{} || (i > 0 && caches[i-1].path == cache.path) {
			return "", errors.New("invalid cache identity")
		}
		_, _ = fmt.Fprintf(h, "%d:%s:%x\n", len(cache.path), cache.path, cache.uuid)
	}
	for _, entry := range entries {
		if !entry.hash {
			continue
		}
		file, err := os.Open(entry.path)
		if err != nil {
			return "", err
		}
		before, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return "", err
		}
		if !before.Mode().IsRegular() || !sameInfo(entry.target, before) {
			_ = file.Close()
			return "", errors.New("runtime changed during verification")
		}
		digest := sha256.New()
		size, readErr := io.Copy(digest, file)
		after, statErr := file.Stat()
		closeErr := file.Close()
		if readErr != nil {
			return "", readErr
		}
		if statErr != nil {
			return "", statErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if size != before.Size() || !sameInfo(before, after) {
			return "", errors.New("runtime changed during verification")
		}
		_, _ = fmt.Fprintf(h, "%d:%s:%d:%x\n", len(entry.path), entry.path, size, digest.Sum(nil))
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
