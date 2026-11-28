package build

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestore_FiltersExtensionsAndRejectsFunctions(t *testing.T) {
	list := "; archive\n1; 3079 1 EXTENSION - vector owner\n2; 3079 2 EXTENSION - ltree owner\n3; 1259 3 TABLE public chunk owner\n"
	got, err := filterRestoreList(list)
	if err != nil || strings.Contains(got, "EXTENSION") || !strings.Contains(got, "TABLE public chunk") {
		t.Fatalf("filtered list %q: %v", got, err)
	}
	for _, bad := range []string{"4; 3079 4 EXTENSION - unsafe owner", "5; 1255 5 FUNCTION public evil() owner", "6; 3466 6 EVENT TRIGGER - evil owner"} {
		if _, err = filterRestoreList(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestFrameworkRestore_RejectsDumpChangedByEitherToolCall(t *testing.T) {
	for _, phase := range []string{"list", "restore"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			dump := filepath.Join(dir, "dump")
			require.NoError(t, os.WriteFile(dump, []byte("invented dump"), 0600))
			script := filepath.Join(dir, "pg_restore")
			body := "#!/bin/sh\nfor last do :; done\nif [ \"$1\" = '-l' ]; then phase=list; printf '%s\\n' '1; 0 0 TABLE public invented builder'; else phase=restore; cat >/dev/null; fi\nif [ \"$phase\" = \"" + phase + "\" ]; then printf changed > \"$last\"; fi\n"
			require.NoError(t, os.WriteFile(script, []byte(body), 0700))
			cfg := Config{BuilderURL: "postgres://builder@127.0.0.1/postgres?sslmode=disable", PgRestore: script}
			sum := sha256.Sum256([]byte("invented dump"))
			err := RestoreFrameworksChecked(context.Background(), cfg, "frameworks_20990101_eval", dump, fmt.Sprintf("%x", sum))
			require.ErrorContains(t, err, "dump changed")
		})
	}
}

func TestFrameworkRestore_ChecksAgainAfterFilteringBeforeRestore(t *testing.T) {
	dir := t.TempDir()
	dump := filepath.Join(dir, "dump")
	original := []byte("invented dump")
	require.NoError(t, os.WriteFile(dump, original, 0600))
	script := filepath.Join(dir, "pg_restore")
	body := "#!/bin/sh\nfor last do :; done\nif [ \"$1\" = '-l' ]; then printf '%s\\n' '1; 0 0 TABLE public invented builder'; else printf invoked > \"$last.marker\"; fi\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0700))
	cfg := Config{BuilderURL: "postgres://builder@127.0.0.1/postgres?sslmode=disable", PgRestore: script}
	check := func() error {
		data, err := os.ReadFile(dump)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, original) {
			return fmt.Errorf("dump changed")
		}
		return nil
	}
	filter := func(list string) (string, error) {
		if err := os.WriteFile(dump, []byte("changed dump"), 0600); err != nil {
			return "", err
		}
		return list, nil
	}
	err := pgRestoreFilteredChecked(context.Background(), cfg, "frameworks_20990101_eval", dump, filter, check)
	require.ErrorContains(t, err, "dump changed")
	require.NoFileExists(t, dump+".marker")
}

func TestFrameworkRestore_ChecksBeforeAndAfterEachInvocation(t *testing.T) {
	dir := t.TempDir()
	dump := filepath.Join(dir, "dump")
	require.NoError(t, os.WriteFile(dump, []byte("invented dump"), 0600))
	script := filepath.Join(dir, "pg_restore")
	body := "#!/bin/sh\nfor last do :; done\nif [ \"$1\" = '-l' ]; then printf L >> \"$last.trace\"; printf '%s\\n' '1; 0 0 TABLE public invented builder'; else printf R >> \"$last.trace\"; fi\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0700))
	cfg := Config{BuilderURL: "postgres://builder@127.0.0.1/postgres?sslmode=disable", PgRestore: script}
	var observed []string
	check := func() error {
		data, err := os.ReadFile(dump + ".trace")
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		observed = append(observed, string(data))
		return nil
	}
	require.NoError(t, pgRestoreFilteredChecked(context.Background(), cfg, "frameworks_20990101_eval", dump, filterFrameworksRestoreList, check))
	require.Equal(t, []string{"", "L", "L", "LR"}, observed)
}
