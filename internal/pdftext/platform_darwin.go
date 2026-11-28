//go:build darwin

package pdftext

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/sys/unix"
)

func readPlatformIdentityContext(ctx context.Context, cacheDirs []string) (platformIdentity, error) {
	if err := ctx.Err(); err != nil {
		return platformIdentity{}, err
	}
	build, err := unix.Sysctl("kern.osversion")
	if err != nil || strings.TrimSpace(build) == "" {
		return platformIdentity{}, errors.New("macOS build unavailable")
	}
	caches, err := cacheIdentitiesContext(ctx, cacheDirs)
	if err != nil {
		return platformIdentity{}, err
	}
	return platformIdentity{build: build, caches: caches}, nil
}
