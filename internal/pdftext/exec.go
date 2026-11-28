package pdftext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	Version     = "26.11.0"
	maxInput    = 50_000_000
	maxStdout   = 50_000_000
	maxStderr   = 64_000
	callTimeout = 60 * time.Second
)

var ErrConfiguration = errors.New("pdf extractor configuration error")
var ErrMemoryLimit = fmt.Errorf("pdf memory limit exceeded: %w", ErrFile)

type Limiter struct {
	slots chan struct{}
}

func NewLimiter() *Limiter { return &Limiter{slots: make(chan struct{}, 2)} }

type Extractor struct {
	path     string
	runtime  runtimeSet
	limiter  *Limiter
	platform func([]string) (platformIdentity, error)
}

func NewExtractor(ctx context.Context, limiter *Limiter) (*Extractor, error) {
	if ctx.Err() != nil || runtime.GOOS != "darwin" || limiter == nil || cap(limiter.slots) != 2 {
		return nil, ErrConfiguration
	}
	path, err := exec.LookPath("pdftotext")
	if err != nil {
		return nil, ErrConfiguration
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(path) {
		return nil, ErrConfiguration
	}
	set, err := resolveRuntime(ctx, path)
	if err != nil {
		return nil, ErrConfiguration
	}
	e := &Extractor{path: path, runtime: set, limiter: limiter, platform: readPlatformIdentity}
	if err := e.checkVersion(ctx); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Extractor) checkVersion(ctx context.Context) error {
	return checkVersionWith(ctx, e.execute)
}

func checkVersionWith(ctx context.Context, execute func(context.Context, []string, []byte, time.Duration, int, int) ([]byte, []byte, error)) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	_, stderr, err := execute(ctx, []string{"-v"}, nil, callTimeout, 64*1024, 64*1024)
	if err != nil || !strings.HasPrefix(string(stderr), "pdftotext version "+Version+"\n") {
		return ErrConfiguration
	}
	return nil
}

func sandboxProfile(path string, set runtimeSet) string {
	var b strings.Builder
	b.WriteString("(version 1) (deny default) (allow file-read-data (literal \"/\")) (allow process-exec (literal ")
	b.WriteString(strconv.Quote(path))
	b.WriteString(")) (allow file-read-metadata ")
	metadata := map[string]bool{"/dev": true}
	all := append(append(append(append(append([]string{path}, set.files...), set.systemFiles...), set.dataDirs...), set.systemDirs...), set.metadata...)
	for _, item := range all {
		for {
			metadata[item] = true
			parent := filepath.Dir(item)
			if parent == item {
				break
			}
			item = parent
		}
	}
	ordered := make([]string, 0, len(metadata))
	for item := range metadata {
		ordered = append(ordered, item)
	}
	sort.Strings(ordered)
	for _, item := range ordered {
		fmt.Fprintf(&b, "(literal %s) ", strconv.Quote(item))
	}
	b.WriteString(") (allow file-read* ")
	for _, file := range append(append([]string{path}, set.files...), set.systemFiles...) {
		fmt.Fprintf(&b, "(literal %s) ", strconv.Quote(file))
	}
	for _, dir := range append(append([]string(nil), set.dataDirs...), set.systemDirs...) {
		fmt.Fprintf(&b, "(subpath %s) ", strconv.Quote(dir))
	}
	b.WriteString("(literal \"/dev/null\") (literal \"/dev/urandom\")) (allow file-write* (literal \"/dev/null\"))")
	return b.String()
}

type boundedOutput struct {
	buf      bytes.Buffer
	limit    int
	exceeded chan<- struct{}
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := w.limit - w.buf.Len()
	if n > remaining {
		select {
		case w.exceeded <- struct{}{}:
		default:
		}
		p = p[:remaining]
	}
	_, _ = w.buf.Write(p)
	return n, nil
}

func (e *Extractor) runPDF(ctx context.Context, args []string, input []byte) ([]byte, error) {
	return e.run(ctx, args, input, callTimeout, maxStdout, maxStderr)
}

func (e *Extractor) run(ctx context.Context, args []string, input []byte, limit time.Duration, stdoutLimit, stderrLimit int) ([]byte, error) {
	stdout, _, err := e.execute(ctx, args, input, limit, stdoutLimit, stderrLimit)
	return stdout, err
}

func (e *Extractor) execute(ctx context.Context, args []string, input []byte, limit time.Duration, stdoutLimit, stderrLimit int) ([]byte, []byte, error) {
	if len(input) > maxInput {
		return nil, nil, ErrFile
	}
	select {
	case e.limiter.slots <- struct{}{}:
		defer func() { <-e.limiter.slots }()
	case <-ctx.Done():
		return nil, nil, ErrFile
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	if ctx.Err() != nil {
		return nil, nil, ErrFile
	}
	p, err := e.start(ctx, args, input, stdoutLimit, stderrLimit)
	if err != nil {
		return nil, nil, err
	}
	return p.wait(ctx)
}

type processResult struct{ err error }

type process struct {
	runtime    runtimeSet
	cmd        *exec.Cmd
	stdout     *boundedOutput
	stderr     *boundedOutput
	exceeded   chan struct{}
	done       <-chan processResult
	observed   bool
	reaped     bool
	sample     func(context.Context, int) (uint64, error)
	input      *os.File
	data       []byte
	writerDone chan processResult
}

func (e *Extractor) start(ctx context.Context, args []string, input []byte, stdoutLimit, stderrLimit int) (*process, error) {
	if ctx.Err() != nil {
		return nil, ErrFile
	}
	if e.runtime.snapshot == nil || checkRuntimeSnapshotContext(ctx, e.runtime, e.runtime.snapshot) != nil {
		if ctx.Err() != nil {
			return nil, ErrFile
		}
		return nil, ErrConfiguration
	}
	argv := append([]string{"-p", sandboxProfile(e.path, e.runtime), e.path}, args...)
	cmd := exec.Command("/usr/bin/sandbox-exec", argv...)
	configureProcess(cmd)
	cmd.Env = []string{"LC_ALL=C"}
	cmd.Dir = "/"
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return nil, ErrConfiguration
	}
	cmd.Stdin = readEnd
	exceeded := make(chan struct{}, 1)
	stdout := &boundedOutput{limit: stdoutLimit, exceeded: exceeded}
	stderr := &boundedOutput{limit: stderrLimit, exceeded: exceeded}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if ctx.Err() != nil {
		_ = readEnd.Close()
		_ = writeEnd.Close()
		return nil, ErrFile
	}
	if startErr := cmd.Start(); startErr != nil {
		_ = readEnd.Close()
		_ = writeEnd.Close()
		return nil, ErrConfiguration
	}
	_ = readEnd.Close()
	done, err := watchProcess(cmd.Process.Pid)
	if err != nil {
		_ = killProcessGroup(cmd.Process.Pid)
		_ = writeEnd.Close()
		_ = cmd.Wait()
		return nil, ErrConfiguration
	}
	p := &process{runtime: e.runtime, cmd: cmd, stdout: stdout, stderr: stderr, exceeded: exceeded, done: done, sample: sampleRSS, input: writeEnd, data: input}
	return p, nil
}

func (p *process) wait(ctx context.Context) ([]byte, []byte, error) {
	return p.waitWithClock(ctx, systemWatchdogClock{})
}

func (p *process) waitWithClock(ctx context.Context, clock watchdogClock) ([]byte, []byte, error) {
	lastSample := clock.Now()
	ticks, stopTicks := clock.Tick()
	defer stopTicks()
	stale, resetStale, stopStale := clock.Timer(time.Second)
	defer stopStale()
	sampleCtx, cancelSample := context.WithCancel(ctx)
	var pending <-chan memoryResult
	defer func() {
		cancelSample()
		if pending != nil {
			<-pending
		}
	}()
	fail := func(err error) ([]byte, []byte, error) {
		_ = p.signal()
		cancelSample()
		return p.kill(err)
	}
	startSample := func() {
		result := make(chan memoryResult, 1)
		pending = result
		go func() {
			rss, err := p.sample(sampleCtx, p.cmd.Process.Pid)
			result <- memoryResult{rss: rss, err: err, completed: clock.Now()}
		}()
	}
	startInput := func() {
		p.writerDone = make(chan processResult, 1)
		go func() {
			_, err := io.Copy(p.input, bytes.NewReader(p.data))
			if closeErr := p.input.Close(); err == nil {
				err = closeErr
			}
			p.writerDone <- processResult{err: err}
		}()
	}
	waitingInput := len(p.data) > 0
	if waitingInput {
		startSample()
	} else {
		_ = p.input.Close()
	}
	applySample := func(result memoryResult) error {
		pending = nil
		if result.err != nil || !result.completed.Before(lastSample.Add(time.Second)) {
			return ErrConfiguration
		}
		if result.rss > 1<<30 {
			return ErrMemoryLimit
		}
		lastSample = result.completed
		resetStale(lastSample.Add(time.Second).Sub(clock.Now()))
		if waitingInput {
			if ctx.Err() != nil {
				return ErrFile
			}
			waitingInput = false
			startInput()
		}
		return nil
	}
	drainSample := func() error {
		select {
		case result := <-pending:
			return applySample(result)
		default:
			return nil
		}
	}
	for {
		if ctx.Err() != nil {
			return fail(ErrFile)
		}
		if err := drainSample(); err != nil {
			return fail(err)
		}
		if !clock.Now().Before(lastSample.Add(time.Second)) {
			return fail(ErrConfiguration)
		}
		select {
		case res := <-p.done:
			p.observed = true
			if err := drainSample(); err != nil {
				return fail(err)
			}
			if !clock.Now().Before(lastSample.Add(time.Second)) || res.err != nil {
				return fail(ErrConfiguration)
			}
			if ctx.Err() != nil {
				return fail(ErrFile)
			}
			waitErr := p.reap()
			var writeErr error
			if p.writerDone != nil {
				writeErr = (<-p.writerDone).err
			}
			if err := checkRuntimeSnapshotContext(ctx, p.runtime, p.runtime.snapshot); err != nil {
				if ctx.Err() != nil {
					return nil, nil, ErrFile
				}
				return nil, nil, ErrConfiguration
			}
			if waitErr != nil || writeErr != nil || ctx.Err() != nil || waitingInput {
				return nil, nil, ErrFile
			}
			select {
			case <-p.exceeded:
				return nil, nil, ErrFile
			default:
			}
			return p.stdout.buf.Bytes(), p.stderr.buf.Bytes(), nil
		case <-ctx.Done():
			return fail(ErrFile)
		case <-p.exceeded:
			return fail(ErrFile)
		case <-stale:
			if err := drainSample(); err != nil {
				return fail(err)
			}
			if !clock.Now().Before(lastSample.Add(time.Second)) {
				return fail(ErrConfiguration)
			}
		case <-ticks:
			if pending == nil && !processExited(p.cmd.Process.Pid) {
				startSample()
			}
		case result := <-pending:
			if err := applySample(result); err != nil {
				return fail(err)
			}
		}
	}
}

func (p *process) signal() error {
	if p.reaped {
		return os.ErrProcessDone
	}
	return killProcessGroup(p.cmd.Process.Pid)
}

func (p *process) reap() error {
	if p.reaped {
		return os.ErrProcessDone
	}
	err := p.cmd.Wait()
	p.reaped = true
	return err
}

func (p *process) kill(reason error) ([]byte, []byte, error) {
	_ = p.signal()
	_ = p.input.Close()
	if !p.observed {
		<-p.done
		p.observed = true
	}
	_ = p.reap()
	if p.writerDone != nil {
		<-p.writerDone
	}
	if err := checkRuntimeSnapshot(p.runtime, p.runtime.snapshot); err != nil {
		return nil, nil, ErrConfiguration
	}
	return nil, nil, reason
}
