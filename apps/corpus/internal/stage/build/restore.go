package build

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

func filterRestoreList(list string) (string, error) {
	return filterRestoreListAllowing(list, nil)
}

func filterRestoreListAllowing(list string, functions map[string]bool) (string, error) {
	var result strings.Builder
	for _, line := range strings.SplitAfter(list, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			result.WriteString(line)
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 4 || !strings.HasSuffix(fields[0], ";") {
			return "", fmt.Errorf("invalid restore list entry")
		}
		switch fields[3] {
		case "EXTENSION":
			if len(fields) < 6 || (fields[5] != "vector" && fields[5] != "ltree") {
				return "", fmt.Errorf("unexpected extension in restore list")
			}
			continue
		case "FUNCTION":
			if len(fields) != 7 || fields[4] != "public" || !functions[fields[5]] {
				return "", fmt.Errorf("function in restore list")
			}
		case "PROCEDURE":
			return "", fmt.Errorf("function in restore list")
		case "EVENT":
			if len(fields) > 4 && fields[4] == "TRIGGER" {
				return "", fmt.Errorf("event trigger in restore list")
			}
		}
		result.WriteString(line)
	}
	return result.String(), nil
}

func pgRestore(ctx context.Context, cfg Config, dbName, dumpPath string) error {
	return pgRestoreFiltered(ctx, cfg, dbName, dumpPath, filterRestoreList)
}

func pgRestoreFiltered(ctx context.Context, cfg Config, dbName, dumpPath string, filter func(string) (string, error)) error {
	return pgRestoreFilteredChecked(ctx, cfg, dbName, dumpPath, filter, nil)
}

func RestoreFrameworksChecked(ctx context.Context, cfg Config, name, path, expected string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(name) || cfg.PgRestore == "" || path == "" || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(expected) {
		return fmt.Errorf("invalid restore configuration")
	}
	check := func() error {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("dump changed")
		}
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("inspect dump: %w", err)
		}
		defer func() { _ = f.Close() }()
		info, err = f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("dump changed")
		}
		sum := sha256.New()
		if _, err = io.Copy(sum, f); err != nil {
			return fmt.Errorf("read dump: %w", err)
		}
		if fmt.Sprintf("%x", sum.Sum(nil)) != expected {
			return fmt.Errorf("dump changed")
		}
		return nil
	}
	return pgRestoreFilteredChecked(ctx, cfg, name, path, filterFrameworksRestoreList, check)
}

func pgRestoreFilteredChecked(ctx context.Context, cfg Config, dbName, dumpPath string, filter func(string) (string, error), check func() error) error {
	env, password, err := toolEnvironment(cfg.BuilderURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	listCmd := exec.CommandContext(ctx, cfg.PgRestore, "-l", dumpPath)
	listCmd.Env = env
	var list, stderr bytes.Buffer
	listCmd.Stdout, listCmd.Stderr = &list, &stderr
	if check != nil {
		if checkErr := check(); checkErr != nil {
			return checkErr
		}
	}
	err = listCmd.Run()
	if check != nil {
		if checkErr := check(); checkErr != nil {
			return checkErr
		}
	}
	if err != nil {
		return toolError("list corpus dump", err, stderr.String(), password, cfg.BuilderURL)
	}
	filtered, err := filter(list.String())
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, cfg.PgRestore, "--no-owner", "--no-privileges", "--exit-on-error", "--use-list", "/dev/stdin", "--dbname", dbName, dumpPath)
	cmd.Env, cmd.Stdin = env, strings.NewReader(filtered)
	stderr.Reset()
	cmd.Stderr = &stderr
	if check != nil {
		if checkErr := check(); checkErr != nil {
			return checkErr
		}
	}
	err = cmd.Run()
	if check != nil {
		if checkErr := check(); checkErr != nil {
			return checkErr
		}
	}
	if err != nil {
		return toolError("restore corpus database", err, stderr.String(), password, cfg.BuilderURL)
	}
	return nil
}
