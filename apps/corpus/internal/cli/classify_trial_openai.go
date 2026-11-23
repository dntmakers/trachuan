package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/stage/build"
)

const openAITrialUsage = "usage: corpus classify trial-openai <cc> <rel> --stage documents|articles --select labels:FILE [--families FILE for articles] --model M --effort E --out DIR --cap-usd N [--dry-run]"

type openAITrialCLI struct {
	trial                   classify.OpenAITrialOptions
	stage, labels, families string
}

func parseOpenAITrial(args []string) (openAITrialCLI, error) {
	var opts openAITrialCLI
	if len(args) < 2 {
		return opts, fmt.Errorf("%s", openAITrialUsage)
	}
	if _, err := build.DatabaseName(args[0], args[1]); err != nil {
		return opts, err
	}
	flags, err := options(args[2:], map[string]bool{"--stage": true, "--select": true, "--families": true, "--model": true, "--effort": true, "--out": true, "--cap-usd": true, "--dry-run": false})
	if err != nil {
		return opts, err
	}
	opts.stage = flags["--stage"]
	opts.labels, _ = strings.CutPrefix(flags["--select"], "labels:")
	if !strings.HasPrefix(flags["--select"], "labels:") || opts.labels == "" || flags["--out"] == "" {
		return opts, fmt.Errorf("trial-openai requires --select labels:FILE and --out DIR")
	}
	opts.families = flags["--families"]
	_, hasFamilies := flags["--families"]
	switch opts.stage {
	case "documents":
		if hasFamilies {
			return opts, fmt.Errorf("--families is only for the articles stage")
		}
	case "articles":
		if opts.families == "" {
			return opts, fmt.Errorf("the articles stage requires --families FILE")
		}
	default:
		return opts, fmt.Errorf("%s", openAITrialUsage)
	}
	model := flags["--model"]
	switch model {
	case "gpt-6.2-sol", "gpt-6.1-terra", "gpt-6-luna":
	default:
		return opts, fmt.Errorf("--model requires gpt-6.2-sol, gpt-6.1-terra or gpt-6-luna")
	}
	r, err := classify.NewOpenAIRecipe(model, strings.Repeat("0", 64))
	if err != nil {
		return opts, err
	}
	r.Effort = flags["--effort"]
	if err = r.Validate(); err != nil {
		return opts, err
	}
	cap, err := parseOpenAITrialCap(flags["--cap-usd"])
	if err != nil {
		return opts, err
	}
	opts.trial = classify.OpenAITrialOptions{Country: args[0], Release: args[1], Recipe: r, Out: flags["--out"], CapMicroUSD: cap, DryRun: flags["--dry-run"] == "true"}
	return opts, nil
}

func parseOpenAITrialCap(value string) (int64, error) {
	const micro = 1_000_000
	invalid := func() (int64, error) {
		return 0, fmt.Errorf("--cap-usd requires a positive decimal up to 40 USD with at most six decimal places")
	}
	whole, fraction, dot := strings.Cut(value, ".")
	if whole == "" || (dot && fraction == "") || len(fraction) > 6 || strings.IndexFunc(whole+fraction, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return invalid()
	}
	usd, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || usd > classify.OpenAITrialMaxMicroUSD/micro {
		return invalid()
	}
	decimal, err := strconv.ParseInt(fraction+strings.Repeat("0", 6-len(fraction)), 10, 64)
	if err != nil {
		return invalid()
	}
	cap := usd*micro + decimal
	if cap < 1 || cap > classify.OpenAITrialMaxMicroUSD {
		return invalid()
	}
	return cap, nil
}

func runClassifyTrialOpenAI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fail := func(code int, err error) int { _, _ = fmt.Fprintln(stderr, err); return code }
	opts, err := parseOpenAITrial(args)
	if err != nil {
		return fail(2, err)
	}
	key, project := os.Getenv("OPENAI_API_KEY"), os.Getenv("OPENAI_PROJECT_ID")
	if strings.TrimSpace(key) == "" || strings.TrimSpace(project) == "" {
		return fail(1, fmt.Errorf("OPENAI_API_KEY and OPENAI_PROJECT_ID are required"))
	}
	for _, value := range []string{project, opts.trial.Out, opts.labels, opts.families} {
		if strings.Contains(value, key) {
			return fail(1, fmt.Errorf("invalid trial credentials or paths"))
		}
	}
	artifacts := os.Getenv("CORPUS_ARTIFACTS")
	if artifacts == "" {
		artifacts = "work/artifacts"
	}
	opts.trial.Out, err = openAITrialOutput(opts.trial.Out, os.Getenv("CORPUS_SUBMISSIONS_DIR"), os.Getenv("CORPUS_CHECKPOINT_DIR"), artifacts)
	if err != nil {
		return fail(2, err)
	}
	client := classify.NewOpenAITrialClient(key, project, nil)
	labels, labelsHash, err := classify.ReadLabels(opts.labels)
	if err != nil {
		return fail(1, err)
	}
	raw, err := os.ReadFile(opts.labels)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != labelsHash {
		return fail(1, fmt.Errorf("trial labels changed while reading"))
	}
	if _, err = classify.VerifyScoreLabels(opts.trial.Recipe, raw); err != nil {
		return fail(1, err)
	}
	opts.trial.LabelsHash, opts.trial.Progress = labelsHash, stderr
	if opts.stage == "articles" {
		b, readErr := os.ReadFile(opts.families)
		if readErr == nil {
			opts.trial.ExpectedFamiliesHash = fmt.Sprintf("%x", sha256.Sum256(b))
		} else if !os.IsNotExist(readErr) {
			return fail(1, fmt.Errorf("read trial families: %w", readErr))
		}
	}
	summary, resumed, err := classify.ResumeOpenAITrial(ctx, opts.stage, opts.trial, client)
	if err == nil && !resumed {
		if opts.stage == "documents" {
			summary, err = runOpenAITrialDocuments(ctx, opts.trial, client, labels)
		} else {
			summary, err = runOpenAITrialArticles(ctx, opts.trial, client, labels, opts.families)
		}
	}
	if encodeErr := json.NewEncoder(stdout).Encode(summary); encodeErr != nil {
		return fail(1, fmt.Errorf("write OpenAI trial summary: %w", encodeErr))
	}
	if err != nil {
		return fail(1, err)
	}
	return 0
}
