package classify

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

type openAICreatorGuard struct {
	*creatorGuard
	id   string
	root unix.Stat_t
}

func (r *Runner) lockOpenAICreator(id string) (*openAICreatorGuard, error) {
	if !validID(id) {
		return nil, fmt.Errorf("invalid creator identity")
	}
	s, err := openCountRoot(r.IntentDir)
	if err != nil {
		return nil, err
	}
	defer s.close()
	var root unix.Stat_t
	if err = unix.Fstat(s.root, &root); err != nil {
		return nil, err
	}
	dir, err := countOpenDir(s.root, "submissions", false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(dir) }()
	fd, err := unix.Openat(dir, id+".creator.lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "creator")
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("invalid creator lock")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &openAICreatorGuard{creatorGuard: &creatorGuard{file: f}, id: id, root: root}, nil
}

func verifyOpenAICreator(root, id string, g *openAICreatorGuard) error {
	s, err := openCountRoot(root)
	if err != nil {
		return err
	}
	defer s.close()
	return verifyOpenAICreatorFD(s.root, id, g)
}

func verifyOpenAICreatorFD(root int, id string, g *openAICreatorGuard) error {
	if g == nil || g.creatorGuard == nil || g.file == nil || g.id != id || !validID(id) {
		return fmt.Errorf("invalid creator guard")
	}
	var rootInfo unix.Stat_t
	if err := unix.Fstat(root, &rootInfo); err != nil || rootInfo.Dev != g.root.Dev || rootInfo.Ino != g.root.Ino {
		return fmt.Errorf("creator root mismatch")
	}
	dir, err := countOpenDir(root, "submissions", false)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(dir) }()
	fd, err := unix.Openat(dir, id+".creator.lock", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "creator")
	defer func() { _ = f.Close() }()
	current, err := f.Stat()
	if err != nil || !current.Mode().IsRegular() {
		return fmt.Errorf("invalid creator lock")
	}
	held, err := g.file.Stat()
	if err != nil || !os.SameFile(current, held) {
		return fmt.Errorf("creator lock mismatch")
	}
	return nil
}
