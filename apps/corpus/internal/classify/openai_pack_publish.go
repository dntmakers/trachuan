package classify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"golang.org/x/sys/unix"
)

func writePackSet(ctx context.Context, store artifact.Store, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if store.Root == "" || len(data) > openAITrialResultLimit {
		return "", fmt.Errorf("invalid classification set store or size")
	}
	rootPath := store.Abs(ClassificationSetNamespace)
	if err := durableDirectory(rootPath); err != nil {
		return "", err
	}
	root, err := openPackDir(rootPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	hash := digest(data)
	if err = unix.Mkdirat(int(root.Fd()), hash, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("create classification digest directory: %w", err)
	}
	dir, err := root.child(hash)
	if err != nil {
		return "", err
	}
	defer func() { _ = dir.Close() }()
	if err = publishPackFile(ctx, root, dir, data); err != nil {
		return "", err
	}
	if err = root.Sync(); err != nil {
		return "", fmt.Errorf("sync classification set namespace: %w", err)
	}
	return hash, nil
}

func publishPackFile(ctx context.Context, root, dir packDir, data []byte) error {
	id, err := newID()
	if err != nil {
		return err
	}
	name := ".classification-" + id
	rootFD := int(root.Fd())
	fd, err := unix.Openat(rootFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC, 0600)
	if err != nil {
		return fmt.Errorf("create temporary classification set: %w", err)
	}
	defer func() { _ = unix.Unlinkat(rootFD, name, 0) }()
	f := os.NewFile(uintptr(fd), name)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return fmt.Errorf("persist classification set: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = unix.Linkat(rootFD, name, int(dir.Fd()), ClassificationSetFilename, 0); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("publish classification set: %w", err)
		}
		prior, readErr := dir.required(ClassificationSetFilename)
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(prior, data) {
			return fmt.Errorf("existing classification set differs at requested digest")
		}
	}
	if err = dir.Sync(); err != nil {
		return fmt.Errorf("sync classification digest directory: %w", err)
	}
	return nil
}
