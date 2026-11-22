package classify

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

var errStoreRootMissing = errors.New("classification intent store root does not exist")

func (r *Runner) withStoreLock(create bool, fn func() error) error {
	if r.IntentDir == "" {
		return fmt.Errorf("classification intent directory is required")
	}
	if create {
		if err := durableDirectory(r.IntentDir); err != nil {
			return err
		}
	}
	dir, err := os.Open(r.IntentDir)
	if !create && errors.Is(err, os.ErrNotExist) {
		return errStoreRootMissing
	}
	if err != nil {
		return fmt.Errorf("open classification intent store: %w", err)
	}
	defer func() { _ = dir.Close() }()
	for {
		err = unix.Flock(int(dir.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("lock classification intent store: %w", err)
	}
	return fn()
}
