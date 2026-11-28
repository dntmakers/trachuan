//go:build linux

package pdftext

import (
	"os"
	"syscall"
)

func sameChangeTime(a, b os.FileInfo) bool {
	left, okLeft := a.Sys().(*syscall.Stat_t)
	right, okRight := b.Sys().(*syscall.Stat_t)
	return okLeft && okRight && left.Ctim.Sec == right.Ctim.Sec && left.Ctim.Nsec == right.Ctim.Nsec
}
