//go:build !darwin && !linux

package pdftext

import "os"

func sameChangeTime(_, _ os.FileInfo) bool { return true }
