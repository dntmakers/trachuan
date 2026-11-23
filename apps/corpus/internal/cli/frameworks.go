package cli

import (
	"context"
	"fmt"
	"io"

	"danny.vn/trachuan/internal/frameworks/build"
)

func runFrameworks(ctx context.Context, args []string, out, errout io.Writer) int {
	if len(args) == 1 && args[0] == "check-titles" || len(args) == 3 && args[0] == "import-titles" && args[1] == "--input" {
		var err error
		if args[0] == "check-titles" {
			err = build.CheckTitles(ctx)
		} else {
			err = build.ImportTitles(ctx, args[2])
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
	if len(args) != 2 || args[0] != "build" {
		_, _ = fmt.Fprintln(errout, "usage: corpus frameworks build <rel> | release-row <rel> | import-titles --input <file> | check-titles")
		return 2
	}
	if err := build.Run(ctx, args[1], out, newFrameworkEmbedder(), newFrameworkDatabase(), openFrameworkCalibration); err != nil {
		_, _ = fmt.Fprintln(errout, err)
		return 1
	}
	return 0
}
