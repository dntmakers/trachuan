//go:build !darwin

package pdftext

import "os/exec"

func watchProcess(_ int) (<-chan processResult, error) { return nil, ErrConfiguration }

func configureProcess(_ *exec.Cmd) {}

func processExited(_ int) bool { return false }

func killProcessGroup(_ int) error { return ErrConfiguration }
