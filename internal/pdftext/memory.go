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
	ctx, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
	defer cancel()
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
