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

const openAITrialUsage = "usage: corpus classify trial-openai <cc> <rel> --stage documents|articles --select labels:FILE|unseeded:I/N [--families FILE for articles] --model M --effort E --out DIR --cap-usd N [--dry-run]"

type openAITrialCLI struct {
	trial                   classify.OpenAITrialOptions
	stage, labels, families string
	part, total             int
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
	selection := flags["--select"]
	if strings.HasPrefix(selection, "labels:") {
		opts.labels = strings.TrimPrefix(selection, "labels:")
	} else if strings.HasPrefix(selection, "unseeded:") {
		part, total, ok := strings.Cut(strings.TrimPrefix(selection, "unseeded:"), "/")
		opts.part, err = strconv.Atoi(part)
		if err != nil || !ok {
			return opts, fmt.Errorf("invalid unseeded selection")
		}
		opts.total, err = strconv.Atoi(total)
		if err != nil || opts.part < 1 || opts.total > 64 || opts.part > opts.total {
			return opts, fmt.Errorf("invalid unseeded selection")
		}
	} else {
		return opts, fmt.Errorf("invalid trial selection")
	}
	if (opts.labels == "" && opts.part == 0) || flags["--out"] == "" {
		return opts, fmt.Errorf("trial-openai requires a selection and --out DIR")
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
	if opts.part != 0 && model != "gpt-6.2-sol" {
		return opts, fmt.Errorf("unseeded selection requires gpt-6.2-sol")
	}
	r, err := classify.NewOpenAIRecipe(model, strings.Repeat("0", 64))
	if err != nil {
		return opts, err
	}
	r.Effort = flags["--effort"]
	if err = r.Validate(); err != nil {
		return opts, err
	}
	cap, err := parseOpenAITrialCapForMode(flags["--cap-usd"], opts.part != 0)
	if err != nil {
		return opts, err
	}
	opts.trial = classify.OpenAITrialOptions{Country: args[0], Release: args[1], Recipe: r, Out: flags["--out"], CapMicroUSD: cap, DryRun: flags["--dry-run"] == "true", Unseeded: opts.part != 0}
	if opts.part != 0 {
		opts.trial.Selection = fmt.Sprintf("unseeded:%d/%d", opts.part, opts.total)
	}
	return opts, nil
}

func parseOpenAITrialCapForMode(value string, unseeded bool) (int64, error) {
	maximum := classify.OpenAITrialMaxCap(unseeded)
	const micro = 1_000_000
	invalid := func() (int64, error) {
		return 0, fmt.Errorf("--cap-usd requires a positive decimal up to %d USD with at most six decimal places", maximum/1_000_000)
	}
	whole, fraction, dot := strings.Cut(value, ".")
	if whole == "" || (dot && fraction == "") || len(fraction) > 6 || strings.IndexFunc(whole+fraction, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return invalid()
	}
	usd, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || usd > maximum/micro {
		return invalid()
	}
	decimal, err := strconv.ParseInt(fraction+strings.Repeat("0", 6-len(fraction)), 10, 64)
	if err != nil {
		return invalid()
	}
	cap := usd*micro + decimal
	if cap < 1 || cap > maximum {
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
	opts.trial.Progress = stderr
	if opts.stage == "articles" {
		b, readErr := os.ReadFile(opts.families)
		if readErr == nil {
			opts.trial.ExpectedFamiliesHash = fmt.Sprintf("%x", sha256.Sum256(b))
		} else if !os.IsNotExist(readErr) {
			return fail(1, fmt.Errorf("read trial families: %w", readErr))
		}
	}
	if opts.part != 0 {
		summary, resumed, resumeErr := classify.ResumeOpenAITrial(ctx, opts.stage, opts.trial, client)
		if resumed || resumeErr != nil {
			if encodeErr := json.NewEncoder(stdout).Encode(summary); encodeErr != nil {
				return fail(1, fmt.Errorf("write OpenAI trial summary: %w", encodeErr))
			}
			if resumeErr != nil {
				return fail(1, resumeErr)
			}
			return 0
		}
	}
	labels, labelsHash, err := openAITrialLabels(ctx, opts)
	if err != nil {
		return fail(1, err)
	}
	opts.trial.LabelsHash = labelsHash
	if opts.part != 0 {
		docs := map[string]bool{}
		for _, a := range labels.Articles {
			docs[a.Key] = true
		}
		opts.trial.SelectedDocuments, opts.trial.SelectedArticles = len(docs), len(labels.Articles)
	}

	summary, resumed, err := classify.ResumeOpenAITrial(ctx, opts.stage, opts.trial, client)
	if err == nil && !resumed {
		if opts.stage == "documents" {
			summary, err = runOpenAITrialDocuments(ctx, opts.trial, client, labels)
		} else {
			summary, err = runOpenAITrialArticles(ctx, opts.trial, client, labels, opts.families)
		}
	}
	if opts.part != 0 && opts.trial.DryRun {
		docs := map[string]bool{}
		for _, a := range labels.Articles {
			docs[a.Key] = true
		}
		summary.SelectedDocuments, summary.SelectedArticles = len(docs), len(labels.Articles)
	}
	if encodeErr := json.NewEncoder(stdout).Encode(summary); encodeErr != nil {
		return fail(1, fmt.Errorf("write OpenAI trial summary: %w", encodeErr))
	}
	if err != nil {
		return fail(1, err)
	}
	return 0
}
