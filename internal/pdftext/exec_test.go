//go:build darwin

package pdftext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func testRuntime() runtimeSet {
	set := runtimeSet{
		systemFiles: []string{"/usr/lib/dyld", "/usr/lib/libSystem.B.dylib", "/usr/lib/libncurses.5.4.dylib"},
		systemDirs:  []string{"/System/Volumes/Preboot/Cryptexes/OS/System/Library/dyld"},
	}
	set.snapshot, _ = snapshotRuntime(set)
	return set
}

func inventedPDF() []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	stream := "BT /F1 12 Tf 72 720 Td (Hello) Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, off := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), start)
	return b.Bytes()
}

func TestExtractor_GeneratedPDF(t *testing.T) {
	e, err := NewExtractor(NewLimiter())
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-layout", "-enc", "UTF-8", "-eol", "unix", "-", "-"}
	a, err := e.runPDF(context.Background(), args, inventedPDF())
	if err != nil || !bytes.Contains(a, []byte("Hello")) {
		t.Fatalf("failed to extract invented PDF: %v", err)
	}
	b, err := e.runPDF(context.Background(), args, inventedPDF())
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("non-deterministic extraction: %v", err)
	}
}

func TestNewExtractor_Version(t *testing.T) {
	if _, err := NewExtractor(NewLimiter()); err != nil {
		t.Fatal(err)
	}
	if err := (&Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}).checkVersion(); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("wrong version accepted: %v", err)
	}
}

func TestRun_BoundsAndRedaction(t *testing.T) {
	tool, err := filepath.EvalSymlinks("/bin/bash")
	if err != nil {
		t.Fatal(err)
	}
	e := &Extractor{path: tool, runtime: testRuntime(), limiter: NewLimiter()}
	for _, tc := range []struct {
		name, script   string
		stdout, stderr int
		wantError      bool
	}{
		{"stdin", "read x; printf %s \"$x\"", 100, 100, false},
		{"stdout", "printf 123456789", 4, 100, true},
		{"stderr", "printf 123456789 >&2", 100, 4, true},
		{"exit", "printf publisher-sentinel >&2; exit 1", 100, 100, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := e.run(context.Background(), []string{"-c", tc.script}, []byte("hello\n"), time.Second, tc.stdout, tc.stderr)
			if tc.wantError != (err != nil) {
				t.Fatalf("error = %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "publisher-sentinel") {
				t.Fatal("extractor output leaked")
			}
			if !tc.wantError && string(out) != "hello" {
				t.Fatalf("output = %q", out)
			}
		})
	}
}

func TestRun_MinimalEnvironment(t *testing.T) {
	t.Setenv("PDFTEXT_PRIVATE_SENTINEL", "must-not-inherit")
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	out, err := e.run(context.Background(), []string{"-c", `printf '%s:%s' "$LC_ALL" "${PDFTEXT_PRIVATE_SENTINEL-unset}"`}, nil, time.Second, 100, 1024)
	if err != nil || string(out) != "C:unset" {
		t.Fatalf("unexpected environment %q: %v", out, err)
	}
}

func TestRun_Cancellation(t *testing.T) {
	tool, err := filepath.EvalSymlinks("/bin/bash")
	if err != nil {
		t.Fatal(err)
	}
	e := &Extractor{path: tool, runtime: testRuntime(), limiter: NewLimiter()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.run(ctx, []string{"-c", "while :; do :; done"}, nil, time.Second, 100, 100); err == nil {
		t.Fatal("cancelled process succeeded")
	}
	if _, err := e.run(context.Background(), []string{"-c", "while :; do :; done"}, nil, 50*time.Millisecond, 100, 100); err == nil {
		t.Fatal("timeout succeeded")
	}
}

func TestProcess_TimeoutKillsProcessGroup(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start([]string{"-c", `printf '%s' "$$"; while :; do :; done`}, nil, 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, _, err = p.wait(ctx)
	if !errors.Is(err, ErrFile) {
		t.Fatalf("timeout: %v", err)
	}
	pid, err := strconv.Atoi(p.stdout.buf.String())
	if err != nil || pid != p.cmd.Process.Pid {
		t.Fatalf("process did not start: %q, stderr %q", p.stdout.buf.String(), p.stderr.buf.String())
	}
	if err := syscall.Kill(-p.cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process group remains: %v", err)
	}
}

func TestProcess_CancellationKillsProcessGroup(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start([]string{"-c", `while :; do :; done`}, nil, 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = p.wait(ctx)
	if !errors.Is(err, ErrFile) {
		t.Fatalf("cancellation: %v", err)
	}
	if err := syscall.Kill(-p.cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process group remains: %v", err)
	}
}

func TestSandbox_DeniesFork(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start([]string{"-c", `while :; do :; done & wait`}, nil, 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, _, err = p.wait(ctx)
	if !errors.Is(err, ErrFile) || !strings.Contains(p.stderr.buf.String(), "fork: Operation not permitted") {
		t.Fatalf("fork denial not verified: %v", err)
	}
}

func TestSandbox_DeniesNetwork(t *testing.T) {
	tool, err := filepath.EvalSymlinks("/bin/bash")
	if err != nil {
		t.Fatal(err)
	}
	e := &Extractor{path: tool, runtime: testRuntime(), limiter: NewLimiter()}
	out, stderr, err := e.execute(context.Background(), []string{"-c", `if : > /dev/tcp/127.0.0.1/1; then printf connected; else printf denied; fi`}, nil, time.Second, 100, 1024)
	if err != nil || string(out) != "denied" || !strings.Contains(string(stderr), "Operation not permitted") {
		t.Fatalf("network denial not verified: output=%q, stderr=%q, err=%v", out, stderr, err)
	}
}

func TestRun_SemaphoreHonorsCancellation(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	e.limiter.slots <- struct{}{}
	e.limiter.slots <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.run(ctx, []string{"-c", "exit 0"}, nil, time.Second, 100, 100); !errors.Is(err, ErrFile) {
		t.Fatalf("cancelled wait: %v", err)
	}
}

func TestSandbox_DeniesRead(t *testing.T) {
	file := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(file, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	tool, err := filepath.EvalSymlinks("/bin/cat")
	if err != nil {
		t.Fatal(err)
	}
	e := &Extractor{path: tool, runtime: testRuntime(), limiter: NewLimiter()}
	if out, err := e.run(context.Background(), nil, []byte("allowed"), time.Second, 100, 100); err != nil || string(out) != "allowed" {
		t.Fatalf("cat did not run: %v", err)
	}
	if _, err := e.run(context.Background(), []string{file}, nil, time.Second, 100, 100); err == nil {
		t.Fatal("sandbox read private file")
	}
}
