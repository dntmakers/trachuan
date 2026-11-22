package build

import (
	"context"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTitleCoverage_ImportAndCheck(t *testing.T) {
	fixture(t)
	input := filepath.Join(t.TempDir(), "input.csv")
	header := "framework_code,version_label,citation_norm,title\n"
	valid := "fw-alpha,v1,A,Family\nfw-alpha,v1,A-2,Control\nfw-alpha,v1,A-2(1),Child\n"
	require.NoError(t, os.WriteFile(input, []byte(header+valid), 0600))
	require.NoError(t, ImportTitles(context.Background(), input))
	require.NoError(t, CheckTitles(context.Background()))
	path := filepath.Join(os.Getenv("FRAMEWORKS_SOURCE_DIR"), "trachuan/titles.csv")
	old, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "code,version,citation,title\nfw-alpha,v1,A,Family\nfw-alpha,v1,A-2,Control\nfw-alpha,v1,A-2(1),Child\n", string(old))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	for _, bad := range []string{valid + "fw-alpha,v1,a -2,Duplicate\n", valid + "fw-alpha,v1,STALE,Stale\n", "fw-alpha,v1,A,Family\n"} {
		require.NoError(t, os.WriteFile(input, []byte(header+bad), 0600))
		require.Error(t, ImportTitles(context.Background(), input))
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, old, after)
	}
	require.NoError(t, os.WriteFile(path, append(old, []byte("fw-alpha,v1,STALE,Stale\n")...), 0600))
	require.Error(t, CheckTitles(context.Background()))
}

func TestParseStep_RejectsStaleTitle(t *testing.T) {
	work := fixture(t)
	path := filepath.Join(os.Getenv("FRAMEWORKS_SOURCE_DIR"), "trachuan/titles.csv")
	require.NoError(t, os.WriteFile(path, []byte("code,version,citation,title\nfw-alpha,v1,STALE,Title\n"), 0600))
	_ = work
	require.Error(t, testRun(context.Background(), "20990101", io.Discard))
}

func TestTitleImport_RejectsInvalidText(t *testing.T) {
	fixture(t)
	path := filepath.Join(os.Getenv("FRAMEWORKS_SOURCE_DIR"), "trachuan/titles.csv")
	old, err := os.ReadFile(path)
	require.NoError(t, err)
	input := filepath.Join(t.TempDir(), "input.csv")
	for _, bad := range []string{"bad\x00title", "bad\xfftitle"} {
		data := "framework_code,version_label,citation_norm,title\nfw-alpha,v1,A," + bad + "\nfw-alpha,v1,A-2,Title\nfw-alpha,v1,A-2(1),Title\n"
		require.NoError(t, os.WriteFile(input, []byte(data), 0600))
		require.Error(t, ImportTitles(context.Background(), input))
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, old, after)
	}
}

func TestTitleImport_ModeUnderUmask(t *testing.T) {
	fixture(t)
	input := filepath.Join(t.TempDir(), "input.csv")
	require.NoError(t, os.WriteFile(input, []byte("framework_code,version_label,citation_norm,title\nfw-alpha,v1,A,Title\nfw-alpha,v1,A-2,Title\nfw-alpha,v1,A-2(1),Title\n"), 0600))
	previous := unix.Umask(0777)
	defer unix.Umask(previous)
	require.NoError(t, ImportTitles(context.Background(), input))
	info, err := os.Stat(filepath.Join(os.Getenv("FRAMEWORKS_SOURCE_DIR"), "trachuan/titles.csv"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestTitleImport_RejectsFIFO(t *testing.T) {
	fixture(t)
	input := filepath.Join(t.TempDir(), "fifo")
	require.NoError(t, unix.Mkfifo(input, 0600))
	done := make(chan error, 1)
	go func() { done <- ImportTitles(context.Background(), input) }()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(80 * time.Millisecond):
		fd, err := unix.Open(input, unix.O_WRONLY|unix.O_NONBLOCK, 0)
		if err == nil {
			_ = unix.Close(fd)
		}
		require.Error(t, <-done)
		t.Fatal("FIFO blocked import")
	}
}

func TestTitles_DisablesCoreDumps(t *testing.T) {
	fixture(t)
	var before unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_CORE, &before))
	if before.Max == 0 {
		t.Skip("core limit already disabled")
	}
	_ = CheckTitles(context.Background())
	var after unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_CORE, &after))
	require.Zero(t, after.Max)
}

type cancelBeforeRename struct {
	context.Context
	checks int
}

func (c *cancelBeforeRename) Err() error {
	c.checks++
	if c.checks >= 8 {
		return context.Canceled
	}
	return nil
}

func TestTitleImport_CancelBeforeRenamePreservesFile(t *testing.T) {
	fixture(t)
	path := filepath.Join(os.Getenv("FRAMEWORKS_SOURCE_DIR"), "trachuan/titles.csv")
	old, err := os.ReadFile(path)
	require.NoError(t, err)
	input := filepath.Join(t.TempDir(), "input.csv")
	require.NoError(t, os.WriteFile(input, []byte("framework_code,version_label,citation_norm,title\nfw-alpha,v1,A,Title\nfw-alpha,v1,A-2,Title\nfw-alpha,v1,A-2(1),Title\n"), 0600))
	ctx := &cancelBeforeRename{Context: context.Background()}
	require.ErrorIs(t, ImportTitles(ctx, input), context.Canceled)
	require.Equal(t, 8, ctx.checks)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, old, after)
}

func TestTitleImport_DisablesCoreDumps(t *testing.T) {
	fixture(t)
	var before unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_CORE, &before))
	if before.Max == 0 {
		t.Skip("core limit already disabled")
	}
	_ = ImportTitles(context.Background(), "missing.csv")
	var after unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_CORE, &after))
	require.Zero(t, after.Max)
}
