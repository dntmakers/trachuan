package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/config"
	"danny.vn/trachuan/apps/corpus/internal/stage/build"
)

type classifyPackCLI struct {
	country, release, runs, baseline string
	parts                            int
}

func parseClassifyPack(args []string) (classifyPackCLI, error) {
	var o classifyPackCLI
	if len(args) < 2 {
		return o, fmt.Errorf("usage: corpus classify pack <cc> <source_rel> --runs DIR --parts N --baseline DIR")
	}
	o.country, o.release = args[0], args[1]
	if _, err := build.DatabaseName(o.country, o.release); err != nil {
		return o, err
	}
	if o.country != "vn" {
		return o, fmt.Errorf("classify pack requires a vn source release and baseline")
	}
	flags, err := options(args[2:], map[string]bool{"--runs": true, "--parts": true, "--baseline": true})
	if err != nil {
		return o, err
	}
	o.runs, o.baseline = flags["--runs"], flags["--baseline"]
	o.parts, err = strconv.Atoi(flags["--parts"])
	if err != nil || o.parts < 1 || o.parts > 64 || o.runs == "" || o.baseline == "" {
		return o, fmt.Errorf("classify pack requires --runs DIR --parts 1..64 --baseline DIR")
	}
	return o, nil
}

func runClassifyPack(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	o, err := parseClassifyPack(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	hash, err := executeClassifyPack(ctx, o)
	if err == nil {
		_, err = fmt.Fprintln(stdout, hash)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func executeClassifyPack(ctx context.Context, o classifyPackCLI) (string, error) {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return "", err
	}
	for _, root := range []string{o.runs, o.baseline} {
		if _, err = openAITrialOutput(root, cfg.Artifacts); err != nil {
			return "", fmt.Errorf("pack archive and artifact store must not overlap: %w", err)
		}
	}
	name, err := build.DatabaseName(o.country, o.release)
	if err != nil {
		return "", err
	}
	source, err := trialReadPool(ctx, cfg.BuilderDatabaseURL, name, "source release")
	if err != nil {
		return "", err
	}
	defer source.Close()
	buildInput, err := classify.LoadOpenAIPackSource(ctx, source, o.country, o.release)
	if err != nil {
		return "", err
	}
	name, err = build.DatabaseName("vn", classify.PackBaselineRelease)
	if err != nil {
		return "", err
	}
	db, err := trialReadPool(ctx, cfg.BuilderDatabaseURL, name, "baseline release")
	if err != nil {
		return "", err
	}
	defer db.Close()
	baseline, err := classify.LoadOpenAIPackBaseline(ctx, db, o.baseline)
	if err != nil {
		return "", err
	}
	return classify.PackOpenAI(ctx, o.runs, o.parts, buildInput, baseline, artifact.Store{Root: cfg.Artifacts})
}
