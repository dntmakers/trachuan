//go:build darwin

package pdftext

import (
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func processExited(pid int) bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }

func killProcessGroup(pid int) error { return syscall.Kill(-pid, syscall.SIGKILL) }

func retryInterrupted(call func() error) error {
	for {
		err := call()
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func watchProcess(pid int) (<-chan processResult, error) {
	fd, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	var event unix.Kevent_t
	unix.SetKevent(&event, pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ENABLE|unix.EV_ONESHOT)
	event.Fflags = unix.NOTE_EXIT
	err = retryInterrupted(func() error {
		_, eventErr := unix.Kevent(fd, []unix.Kevent_t{event}, nil, nil)
		return eventErr
	})
	done := make(chan processResult, 1)
	if errors.Is(err, unix.ESRCH) {
		_ = unix.Close(fd)
		done <- processResult{}
		return done, nil
	}
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	go func() {
		defer func() { _ = unix.Close(fd) }()
		var events [1]unix.Kevent_t
		var count int
		waitErr := retryInterrupted(func() error {
			var err error
			count, err = unix.Kevent(fd, nil, events[:], nil)
			return err
		})
		if waitErr == nil && (count != 1 || events[0].Fflags&unix.NOTE_EXIT == 0) {
			waitErr = errors.New("invalid process event")
		}
		if waitErr == nil && events[0].Flags&unix.EV_ERROR != 0 {
			waitErr = syscall.Errno(events[0].Data)
		}
		done <- processResult{err: waitErr}
	}()
	return done, nil
}
