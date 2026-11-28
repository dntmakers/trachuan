package pdftext

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type cacheIdentity struct {
	path string
	uuid [16]byte
}

type platformIdentity struct {
	build  string
	caches []cacheIdentity
}

func readCacheUUID(path string) ([16]byte, error) {
	var uuid [16]byte
	file, err := os.Open(path)
	if err != nil {
		return uuid, err
	}
	defer func() { _ = file.Close() }()
	var magic [16]byte
	if _, err := file.ReadAt(magic[:], 0); err != nil {
		return uuid, err
	}
	if !bytes.HasPrefix(magic[:], []byte("dyld_v")) {
		return uuid, errors.New("invalid dyld cache magic")
	}
	// https://github.com/apple-oss-distributions/dyld/blob/main/include/mach-o/dyld_cache_format.h defines the UUID at offset 88.
	if _, err := file.ReadAt(uuid[:], 88); err != nil {
		return uuid, err
	}
	if uuid == [16]byte{} {
		return uuid, errors.New("missing dyld cache UUID")
	}
	return uuid, nil
}

func cacheIdentities(dirs []string) ([]cacheIdentity, error) {
	return cacheIdentitiesContext(context.Background(), dirs)
}

func cacheIdentitiesContext(ctx context.Context, dirs []string) ([]cacheIdentity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var caches []cacheIdentity
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			name := entry.Name()
			if strings.HasSuffix(name, ".map") || strings.HasSuffix(name, ".atlas") {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				info, err := os.Stat(path)
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					return nil
				}
			} else if !entry.Type().IsRegular() {
				return nil
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			var magic [6]byte
			_, readErr := io.ReadFull(file, magic[:])
			closeErr := file.Close()
			if closeErr != nil {
				return closeErr
			}
			isNamedCache := strings.HasPrefix(name, "dyld_shared_cache_")
			if readErr != nil || string(magic[:]) != "dyld_v" {
				if isNamedCache {
					return errors.New("invalid dyld cache")
				}
				return nil
			}
			if seen[path] {
				return nil
			}
			uuid, err := readCacheUUID(path)
			if err != nil {
				return err
			}
			seen[path] = true
			caches = append(caches, cacheIdentity{path: path, uuid: uuid})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(caches) == 0 {
		return nil, errors.New("no dyld caches")
	}
	sort.Slice(caches, func(i, j int) bool { return caches[i].path < caches[j].path })
	return caches, nil
}
