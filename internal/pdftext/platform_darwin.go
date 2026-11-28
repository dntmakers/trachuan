//go:build darwin

package pdftext

import (
	"errors"
	"strings"

	"golang.org/x/sys/unix"
)

func readPlatformIdentity(cacheDirs []string) (platformIdentity, error) {
	build, err := unix.Sysctl("kern.osversion")
	if err != nil || strings.TrimSpace(build) == "" {
		return platformIdentity{}, errors.New("macOS build unavailable")
	}
	caches, err := cacheIdentities(cacheDirs)
	if err != nil {
		return platformIdentity{}, err
	}
	return platformIdentity{build: build, caches: caches}, nil
}
