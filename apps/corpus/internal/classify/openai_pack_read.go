package classify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

type packDir struct{ *os.File }

func openPackDir(path string) (packDir, error) {
	return openPackDirAt(unix.AT_FDCWD, filepath.Clean(path), path)
}

func openPackDirAt(parent int, name, path string) (packDir, error) {
	fd, err := unix.Openat(parent, name, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return packDir{}, fmt.Errorf("open %s: %w", path, err)
	}
	return packDir{os.NewFile(uintptr(fd), path)}, nil
}

func (d packDir) child(name string) (packDir, error) {
	return openPackDirAt(int(d.Fd()), name, filepath.Join(d.Name(), name))
}

func (d packDir) names() ([]string, error) {
	listing, err := d.child(".")
	if err != nil {
		return nil, err
	}
	defer func() { _ = listing.Close() }()
	return listing.Readdirnames(-1)
}

func (d packDir) lock() error {
	for {
		err := unix.Flock(int(d.Fd()), unix.LOCK_SH|unix.LOCK_NB)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func (d packDir) read(name string) ([]byte, bool, error) {
	if filepath.Base(name) != name || name == "." || name == ".." {
		return nil, false, fmt.Errorf("invalid trial file name")
	}
	fd, err := unix.Openat(int(d.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open %s: %w", name, err)
	}
	return readTrialFD(fd, name)
}

func (d packDir) required(name string) ([]byte, error) {
	raw, found, err := d.read(name)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("missing %s", name)
	}
	return raw, nil
}

func (d packDir) bound(name, want string) ([]byte, error) {
	raw, err := d.required(name)
	if err != nil {
		return nil, err
	}
	if digest(raw) != want {
		return nil, fmt.Errorf("%s SHA-256 differs from state", name)
	}
	return raw, nil
}

func (d packDir) output(name string) ([]byte, string, error) {
	manifest, err := d.required("MANIFEST.sha256")
	if err != nil {
		return nil, "", err
	}
	raw, err := d.required(name)
	if err != nil {
		return nil, "", err
	}
	hash := digest(raw)
	if string(manifest) != hash+"  "+name+"\n" {
		return nil, "", fmt.Errorf("MANIFEST.sha256 does not bind exactly %s", name)
	}
	if packCheckJSON(raw) != nil {
		return nil, "", fmt.Errorf("invalid or ambiguous JSON in %s", name)
	}
	return raw, hash, nil
}

func decodePackJSON(name string, raw []byte, dst any) error {
	if packCheckJSON(raw) != nil || validateRetainedJSON(raw, dst) != nil || json.Unmarshal(raw, dst) != nil {
		return fmt.Errorf("invalid or ambiguous JSON in %s", name)
	}
	return nil
}

func packCheckJSON(raw []byte) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("invalid JSON encoding")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := packJSONValue(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func packJSONValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return fmt.Errorf("JSON nesting limit")
	}
	tok, err := d.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			key, keyErr := d.Token()
			name, ok := key.(string)
			if keyErr != nil || !ok || seen[name] {
				return fmt.Errorf("invalid or duplicate JSON key")
			}
			seen[name] = true
			if err = packJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	case json.Delim('['):
		for d.More() {
			if err = packJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	_, err = d.Token()
	return err
}

func packSameRecord(raw []byte, expected any) bool {
	encoded, err := json.Marshal(expected)
	if err != nil {
		return false
	}
	decode := func(data []byte) (any, error) {
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		var value any
		decodeErr := d.Decode(&value)
		return value, decodeErr
	}
	actual, err := decode(raw)
	if err != nil {
		return false
	}
	want, err := decode(encoded)
	return err == nil && packEqualJSON(actual, want)
}

func packEqualJSON(a, b any) bool {
	left, lerr := json.Marshal(a)
	right, rerr := json.Marshal(b)
	return lerr == nil && rerr == nil && bytes.Equal(left, right)
}
