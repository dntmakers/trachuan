package classify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"golang.org/x/sys/unix"
)

type openAICountStore struct {
	parent, root, counts, journal int
	syncDir                       func(int) error
}

func countOpenDir(parent int, name string, create bool) (int, error) {
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) && create {
		if err = unix.Mkdirat(parent, name, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
			return -1, err
		}
		if err = unix.Fsync(parent); err != nil {
			return -1, err
		}
		fd, err = unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	}
	return fd, err
}
func openCountRoot(root string) (*openAICountStore, error) {
	if root == "" {
		return nil, fmt.Errorf("classification intent directory is required")
	}
	root = filepath.Clean(root)
	parent, err := unix.Open(filepath.Dir(root), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	fd, err := countOpenDir(parent, filepath.Base(root), false)
	if err != nil {
		_ = unix.Close(parent)
		return nil, err
	}
	return &openAICountStore{parent: parent, root: fd, counts: -1, journal: -1, syncDir: unix.Fsync}, nil
}
func (s *openAICountStore) openDirectories(create bool) error {
	var err error
	s.counts, err = countOpenDir(s.root, "counts", create)
	if err != nil {
		return err
	}
	s.journal, err = countOpenDir(s.counts, "openai-calls", create)
	if errors.Is(err, unix.ENOENT) && !create {
		return nil
	}
	return err
}
func openCountStore(root string, create bool) (*openAICountStore, error) {
	s, err := openCountRoot(root)
	if err != nil {
		return nil, err
	}
	if err = s.openDirectories(create); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}
func (s *openAICountStore) close() {
	for _, fd := range []int{s.journal, s.counts, s.root, s.parent} {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
	}
}
func (r *Runner) withOpenAICountStore(create bool, syncDir func(int) error, fn func(*openAICountStore) error) error {
	s, err := openCountRoot(r.IntentDir)
	if errors.Is(err, unix.ENOENT) {
		return errStoreRootMissing
	}
	if err != nil {
		return err
	}
	defer s.close()
	for {
		err = unix.Flock(s.root, unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return err
	}
	s.syncDir = syncDir
	if err = s.openDirectories(create); err != nil {
		if errors.Is(err, unix.ENOENT) && !create {
			return fn(s)
		}
		return err
	}
	return fn(s)
}
func countReadFile(dir int, name string) ([]byte, error) {
	fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid count evidence file")
	}
	b, err := io.ReadAll(io.LimitReader(f, 4<<20+1))
	if err != nil || len(b) > 4<<20 {
		return nil, fmt.Errorf("invalid count evidence file")
	}
	return b, nil
}
func countEntries(fd int) ([]os.DirEntry, error) {
	copyFD, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(copyFD), "counts")
	defer func() { _ = f.Close() }()
	return f.ReadDir(-1)
}
func countDecode(raw []byte, value any, names ...string) error {
	fields, err := batchapi.DecodeEvidenceObject(raw, names...)
	if err != nil || len(fields) != len(names) || !openAIAllowed(fields, names...) || validateRetainedJSON(raw, value) != nil || json.Unmarshal(raw, value) != nil {
		return fmt.Errorf("invalid count evidence")
	}
	return nil
}
func (s *openAICountStore) snapshot() (map[string]int64, []openAICountPair, error) {
	counts := map[string]int64{}
	entries, err := countEntries(s.counts)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		if entry.Name() == "openai-calls" && entry.IsDir() && s.journal >= 0 {
			continue
		}
		key, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || !validKey(key) || !entry.Type().IsRegular() {
			return nil, nil, fmt.Errorf("invalid count path")
		}
		raw, e := countReadFile(s.counts, entry.Name())
		if e != nil {
			return nil, nil, e
		}
		fields, e := batchapi.DecodeEvidenceObject(raw, "key", "input_tokens")
		if e != nil {
			return nil, nil, fmt.Errorf("invalid count")
		}
		var stored string
		n, valid := batchapi.TokenInteger(fields["input_tokens"], 1)
		if json.Unmarshal(fields["key"], &stored) != nil || stored != key || !valid {
			return nil, nil, fmt.Errorf("invalid count")
		}
		counts[key] = n
	}
	pairs, err := s.journalSnapshot()
	return counts, pairs, err
}
func (s *openAICountStore) journalSnapshot() ([]openAICountPair, error) {
	if s.journal < 0 {
		return nil, nil
	}
	entries, err := countEntries(s.journal)
	if err != nil {
		return nil, err
	}
	dispatches := map[string]openAICountDispatch{}
	results := map[string]openAICountResult{}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".staging" && entry.IsDir() {
			fd, e := countOpenDir(s.journal, name, false)
			if e != nil {
				return nil, e
			}
			_ = unix.Close(fd)
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, fmt.Errorf("invalid OpenAI count evidence")
		}
		raw, e := countReadFile(s.journal, name)
		if e != nil {
			return nil, e
		}
		if id, ok := strings.CutSuffix(name, ".dispatch.json"); ok {
			var d openAICountDispatch
			if !validID(id) || countDecode(raw, &d, "id", "key", "project", "fingerprint", "model", "version") != nil || d.ID != id || !openAIDispatchValid(d) {
				return nil, fmt.Errorf("invalid OpenAI dispatch")
			}
			dispatches[id] = d
		} else if id, ok := strings.CutSuffix(name, ".result.json"); ok {
			var result openAICountResult
			if !validID(id) || countDecode(raw, &result, "id", "key", "input_tokens", "unknown") != nil || result.ID != id || !validKey(result.Key) || (result.Unknown && result.InputTokens != 0) || (!result.Unknown && result.InputTokens < 1) {
				return nil, fmt.Errorf("invalid OpenAI result")
			}
			results[id] = result
		} else {
			return nil, fmt.Errorf("invalid OpenAI evidence")
		}
	}
	for id, result := range results {
		d, ok := dispatches[id]
		if !ok || d.Key != result.Key {
			return nil, fmt.Errorf("unpaired OpenAI evidence")
		}
	}
	pairs := make([]openAICountPair, 0, len(dispatches))
	for id, d := range dispatches {
		p := openAICountPair{dispatch: d}
		if result, ok := results[id]; ok {
			p.result = &result
		}
		pairs = append(pairs, p)
	}
	return pairs, nil
}
func (s *openAICountStore) write(dir int, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return publishEvidenceOnce(s.journal, dir, name, raw, s.syncDir)
}
func (s *openAICountStore) barrier() error {
	for _, fd := range []int{s.journal, s.counts, s.root, s.parent} {
		if fd >= 0 {
			if err := s.syncDir(fd); err != nil {
				return err
			}
		}
	}
	return nil
}
func (r *Runner) countSnapshot(create bool, syncDir func(int) error, fn func(*openAICountStore, map[string]int64, []openAICountPair) error) error {
	return r.withOpenAICountStore(create, syncDir, func(s *openAICountStore) error {
		if err := s.barrier(); err != nil {
			return err
		}
		if s.counts < 0 {
			return fn(s, nil, nil)
		}
		counts, pairs, err := s.snapshot()
		if err != nil {
			return err
		}
		return fn(s, counts, pairs)
	})
}
func countKnown(key string, counts map[string]int64, pairs []openAICountPair) (int64, error) {
	var known int64
	for _, p := range pairs {
		if p.dispatch.Key == key && p.result != nil && !p.result.Unknown {
			n := p.result.InputTokens
			if known != 0 && known != n {
				return 0, fmt.Errorf("conflicting OpenAI counts")
			}
			known = n
		}
	}
	if cached := counts[key]; cached != 0 && (known == 0 || known != cached) {
		return 0, fmt.Errorf("unauthenticated OpenAI count cache")
	}
	return known, nil
}
