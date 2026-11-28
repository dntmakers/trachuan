package cli

import (
	"context"
	"fmt"
	"io"

	"danny.vn/trachuan/internal/frameworks/build"
	frameworkcommand "danny.vn/trachuan/internal/frameworks/command"
)

func runFrameworks(ctx context.Context, args []string, out, errout io.Writer) int {
	runtime := build.NewLazyPDFRuntime(ctx)
	if len(args) == 1 && args[0] == "check-titles" || len(args) == 3 && args[0] == "import-titles" && args[1] == "--input" {
		var err error
		if args[0] == "check-titles" {
			err = build.CheckTitlesWithExtractor(ctx, runtime)
		} else {
			err = build.ImportTitlesWithExtractor(ctx, args[2], runtime)
		}
		if err != nil {
			_, _ = fmt.Fprintln(errout, "frameworks titles command failed")
			return 1
		}
		return 0
	}
	if len(args) == 2 && args[0] == "release-row" {
		return runFrameworksReleaseRow(ctx, args[1], errout)
	}
	if len(args) == 1 && args[0] == "edges" || len(args) == 2 && args[0] != "build" && args[0] != "release-row" && args[0] != "embed" {
		if err := frameworkcommand.RunWithExtractor(ctx, args, out, runtime); err != nil {
			_, _ = fmt.Fprintln(errout, err)
			return 1
		}
		return 0
	}
	if len(args) != 2 || args[0] != "build" {
		_, _ = fmt.Fprintln(errout, "usage: corpus frameworks build <rel> | release-row <rel> | import-titles --input <file> | check-titles")
		return 2
	}
	if err := build.RunWithOptions(ctx, args[1], out, newFrameworkEmbedder(), newFrameworkDatabase(), build.RunOptions{PDFRuntime: runtime}, openFrameworkCalibration); err != nil {
		_, _ = fmt.Fprintln(errout, err)
		return 1
	}
	return 0
}
