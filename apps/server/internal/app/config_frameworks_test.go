package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_FrameworksRejectsInvalidBoolean(t *testing.T) {
	t.Setenv("SERVER_AUTH_DATABASE_URL", "postgres://localhost/auth")
	t.Setenv("SERVER_CORPUS_DATABASE_URL", "postgres://localhost/{database}")
	t.Setenv("SERVER_PUBLIC_URL", "http://localhost:8080")
	for _, raw := range []string{"", "1", "0", "TRUE", "False", " true", "false ", "yes"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("SERVER_FRAMEWORKS", raw)
			if _, err := loadConfig(); err == nil || err.Error() != "invalid SERVER_FRAMEWORKS" {
				t.Fatalf("value %q: error=%v", raw, err)
			}
		})
	}
}

func TestConfig_FrameworksEntitledRequiresKey(t *testing.T) {
	t.Setenv("SERVER_AUTH_DATABASE_URL", "postgres://localhost/auth")
	t.Setenv("SERVER_CORPUS_DATABASE_URL", "postgres://localhost/{database}")
	t.Setenv("SERVER_PUBLIC_URL", "http://localhost:8080")
	t.Setenv("SERVER_FRAMEWORKS", "entitled")
	t.Setenv("SERVER_FRAMEWORKS_KEY_FILE", "")
	if _, err := loadConfig(); err == nil || err.Error() != "invalid or unreadable SERVER_FRAMEWORKS_KEY_FILE" {
		t.Fatalf("missing key: %v", err)
	}
}

func TestConfig_FrameworksDefaultsAndValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte(strings.Repeat("s", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SERVER_FRAMEWORKS_KEY_FILE", path)
	t.Setenv("SERVER_AUTH_DATABASE_URL", "postgres://localhost/auth")
	t.Setenv("SERVER_CORPUS_DATABASE_URL", "postgres://localhost/{database}")
	t.Setenv("SERVER_PUBLIC_URL", "http://localhost:8080")
	t.Setenv("SERVER_FRAMEWORKS", "false")
	if err := os.Unsetenv("SERVER_FRAMEWORKS"); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil || cfg.frameworks || cfg.frameworksMode != "false" {
		t.Fatalf("default enabled=%v mode=%q error=%v", cfg.frameworks, cfg.frameworksMode, err)
	}
	for _, raw := range []string{"true", "false", "entitled"} {
		t.Setenv("SERVER_FRAMEWORKS", raw)
		cfg, err = loadConfig()
		if err != nil || cfg.frameworks != (raw != "false") || cfg.frameworksMode != raw {
			t.Fatalf("value %q: enabled=%v mode=%q error=%v", raw, cfg.frameworks, cfg.frameworksMode, err)
		}
	}
}
