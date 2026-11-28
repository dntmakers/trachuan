//go:build darwin

package pdftext

import (
	"os"
	"syscall"
)

func sameChangeTime(a, b os.FileInfo) bool {
	left, okLeft := a.Sys().(*syscall.Stat_t)
	right, okRight := b.Sys().(*syscall.Stat_t)
	return okLeft && okRight && left.Ctimespec.Sec == right.Ctimespec.Sec && left.Ctimespec.Nsec == right.Ctimespec.Nsec
}
