package pdftext

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func sampleRSS(ctx context.Context, pid int) (uint64, error) {
	cmd := exec.CommandContext(ctx, "/bin/ps", "-o", "rss=", "-p", strconv.Itoa(pid))
	cmd.Env = []string{"LC_ALL=C"}
	output, err := cmd.Output()
	if err != nil {
		return 0, errors.New("memory sampler unavailable")
	}
	fields := strings.Fields(string(output))
	if len(fields) != 1 {
		return 0, errors.New("invalid memory sample")
	}
	kib, err := strconv.ParseUint(fields[0], 10, 54)
	if err != nil {
		return 0, errors.New("invalid memory sample")
	}
	return kib * 1024, nil
}

type memoryResult struct {
	rss       uint64
	err       error
	completed time.Time
}

type watchdogClock interface {
	Now() time.Time
	Tick() (<-chan time.Time, func())
	Timer(time.Duration) (<-chan time.Time, func(time.Duration), func())
}

type systemWatchdogClock struct{}

func (systemWatchdogClock) Now() time.Time { return time.Now() }
func (systemWatchdogClock) Tick() (<-chan time.Time, func()) {
	t := time.NewTicker(50 * time.Millisecond)
	return t.C, t.Stop
}
func (systemWatchdogClock) Timer(d time.Duration) (<-chan time.Time, func(time.Duration), func()) {
	t := time.NewTimer(d)
	reset := func(d time.Duration) {
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		t.Reset(d)
	}
	return t.C, reset, func() { t.Stop() }
}
