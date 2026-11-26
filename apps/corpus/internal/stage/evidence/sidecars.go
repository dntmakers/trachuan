package evidence

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
	"github.com/jackc/pgx/v5/pgxpool"
)

func (s *Stage) summaryFor(pool *pgxpool.Pool) summarySource {
	if s.summary != nil {
		return s.summary(pool)
	}
	return releasechange.NewSQLRelease(pool)
}

func writeFile(path string, write func(io.Writer) error) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	writeErr := write(f)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", filepath.Base(path), closeErr)
	}
	return nil
}

func writeSidecars(ctx context.Context, dir, rel string, inputs frozenInputs, m members, src summarySource) error {
	identity := filepath.Join(dir, identityFile)
	if err := writeFile(identity, func(w io.Writer) error {
		return writeIdentity(ctx, w, src, m, inputs.identityBindings(rel))
	}); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, changesFile), func(w io.Writer) error {
		return releasechange.WriteChanges(w, releasechange.ChangeInputs{Current: identity, Predecessor: inputs.predecessor.identityPath, Members: m, Bindings: inputs.changesBindings(rel)})
	})
}

func readHeader(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return refuse("open %s: %v", filepath.Base(path), err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<12), lineLimit+1)
	if !scanner.Scan() {
		return refuse("%s has no header", filepath.Base(path))
	}
	if err = strictDecode(scanner.Bytes(), out); err != nil {
		return refuse("%s header: %v", filepath.Base(path), err)
	}
	return nil
}

func checkSidecars(dir, rel string, inputs frozenInputs) error {
	var identity identityHeader
	if err := readHeader(filepath.Join(dir, identityFile), &identity); err != nil {
		return err
	}
	if identity.Schema != summarySchema || identity.Bindings != inputs.identityBindings(rel) {
		return errors.New("identity summary does not bind the run")
	}
	var changes changesHeader
	if err := readHeader(filepath.Join(dir, changesFile), &changes); err != nil {
		return err
	}
	want := changesHeader{Schema: summarySchema, Bindings: inputs.changesBindings(rel), Baseline: inputs.predecessor.identityPath == ""}
	if changes != want {
		return errors.New("changes summary does not bind the run")
	}
	return nil
}
