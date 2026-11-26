package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"danny.vn/trachuan/apps/server/internal/releasecheck"
	"danny.vn/trachuan/internal/embedding"
)

func runReleaseCheck(ctx context.Context, args []string, stdout io.Writer) error {
	kind, database, expected, err := releaseCheckArguments(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	base := os.Getenv("SERVER_QUERY_EMBEDDER_URL")
	if base == "" {
		base = "http://127.0.0.1:8091"
	}
	client := embedding.NewClient(base, &http.Client{Timeout: 10 * time.Second})
	if kind == "frameworks" {
		var report any
		if database == "" {
			report, err = releasecheck.DescribeFrameworks(ctx, client)
		} else {
			report, err = releasecheck.CheckFrameworks(ctx, os.Getenv("SERVER_CORPUS_DATABASE_URL"), database, client)
		}
		if err != nil {
			return err
		}
		if err = json.NewEncoder(stdout).Encode(report); err != nil {
			return fmt.Errorf("write frameworks report: %w", err)
		}
		return nil
	}
	report, err := releasecheck.Describe(ctx, client)
	if err != nil {
		return fmt.Errorf("describe release compatibility: %w", err)
	}
	if database != "" {
		report.ChangesProjection, err = releasecheck.Check(ctx, os.Getenv("SERVER_CORPUS_DATABASE_URL"), database, client, expected)
		if err != nil {
			return fmt.Errorf("check corpus release: %w", err)
		}
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		return fmt.Errorf("write compatibility report: %w", err)
	}
	return nil
}

func releaseCheckArguments(args []string) (string, string, *releasecheck.ProjectionExpected, error) {
	values := map[string]string{}
	invalid := func() (string, string, *releasecheck.ProjectionExpected, error) {
		return "", "", nil, errors.New("invalid release-check arguments")
	}
	for len(args) > 0 {
		if len(args) < 2 {
			return invalid()
		}
		if _, ok := values[args[0]]; ok {
			return invalid()
		}
		switch args[0] {
		case "--kind", "--database", "--projection-version", "--projection-sha256", "--projection-release", "--projection-previous":
			values[args[0]] = args[1]
		default:
			return invalid()
		}
		args = args[2:]
	}
	kind, database := values["--kind"], values["--database"]
	if _, ok := values["--kind"]; ok && kind != "frameworks" {
		return invalid()
	}
	if _, ok := values["--database"]; ok && ((kind == "" && !releasecheck.ValidDatabase(database)) || (kind == "frameworks" && !releasecheck.ValidFrameworksDatabase(database))) {
		return invalid()
	}
	flags := []string{"--projection-version", "--projection-sha256", "--projection-release", "--projection-previous"}
	count := 0
	for _, flag := range flags {
		if _, ok := values[flag]; ok {
			count++
		}
	}
	if count == 0 {
		return kind, database, nil, nil
	}
	if count != len(flags) || database == "" || kind != "" {
		return invalid()
	}
	version, err := strconv.Atoi(values[flags[0]])
	if err != nil || version < 1 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(values[flags[1]]) || !releasecheck.ValidDatabase("corpus_vn_"+values[flags[2]]) || (values[flags[3]] != "" && !releasecheck.ValidDatabase("corpus_vn_"+values[flags[3]])) {
		return invalid()
	}
	return kind, database, &releasecheck.ProjectionExpected{Version: version, SHA256: values[flags[1]], Release: values[flags[2]], Previous: values[flags[3]]}, nil
}
