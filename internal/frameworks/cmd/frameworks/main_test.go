package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSignalCancelsAndReaps(t *testing.T) {
	if os.Getenv("FRAMEWORKS_SIGNAL_TEST") == "1" {
		err := runWithSignals(func(ctx context.Context) error {
			child := exec.Command("sleep", "60")
			if err := child.Start(); err != nil {
				return err
			}
			fmt.Printf("ready %d\n", child.Process.Pid)
			<-ctx.Done()
			if err := child.Process.Kill(); err != nil {
				return err
			}
			_ = child.Wait()
			return nil
		})
		if err != nil {
			os.Exit(2)
		}
		return
	}
	child := exec.Command(os.Args[0], "-test.run=^TestSignalCancelsAndReaps$")
	child.Env = append(os.Environ(), "FRAMEWORKS_SIGNAL_TEST=1")
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	lines := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	var pid int
	select {
	case line := <-lines:
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "ready" {
			t.Fatalf("unexpected helper output %q", line)
		}
		pid, err = strconv.Atoi(fields[1])
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not start")
	}
	defer func() {
		if pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}()
	if err = child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not exit")
	}
	if err = syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("child still present: %v", err)
	}
	pid = 0
}
