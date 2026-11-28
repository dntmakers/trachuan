package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"danny.vn/trachuan/internal/frameworks/build"

	"danny.vn/trachuan/internal/frameworks/command"
)

func runWithSignals(run func(context.Context) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx)
}

func main() {
	err := runWithSignals(func(ctx context.Context) error {
		return command.RunWithExtractor(ctx, os.Args[1:], os.Stdout, build.NewLazyPDFRuntime(ctx))
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
