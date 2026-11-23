package classify

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"
)

func submitNamespace(sub int, id string) error {
	entries, err := countEntries(sub)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		n := entry.Name()
		if strings.HasPrefix(n, id) && n != id+".json" && n != id+".creator.lock" {
			return fmt.Errorf("prior submission evidence")
		}
	}
	return nil
}

func submitOtherEvidence(root int, id string) error {
	evidence, err := countOpenDir(root, "evidence", false)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(evidence) }()
	intent, err := countOpenDir(evidence, id, false)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(intent) }()
	entries, err := countEntries(intent)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != "openai-submit" {
			return fmt.Errorf("prior intent evidence")
		}
	}
	return nil
}

func submitResultEvidence(root int, id string) error {
	results, err := countOpenDir(root, "results", false)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(results) }()
	dirs, err := countEntries(results)
	if err != nil {
		return err
	}
	for _, entry := range dirs {
		dir, e := countOpenDir(results, entry.Name(), false)
		if e != nil {
			return e
		}
		entries, e := countEntries(dir)
		_ = unix.Close(dir)
		if e != nil {
			return e
		}
		for _, item := range entries {
			if strings.HasPrefix(item.Name(), id) {
				return fmt.Errorf("prior result evidence")
			}
		}
	}
	return nil
}

func submitPrefix(dir, n int) error {
	if dir < 0 {
		if n == 0 {
			return nil
		}
		return fmt.Errorf("missing evidence")
	}
	entries, err := countEntries(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".staging" {
			fd, e := countOpenDir(dir, name, false)
			if e != nil {
				return e
			}
			_ = unix.Close(fd)
			continue
		}
		allowed := false
		for i := 0; i < n; i++ {
			if name == submitNames[i] {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("unexpected submission evidence")
		}
	}
	return nil
}
