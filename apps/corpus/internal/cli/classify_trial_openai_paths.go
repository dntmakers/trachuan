package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func openAITrialOutput(path string, forbidden ...string) (string, error) {
	out, err := resolvedPath(path)
	if err != nil {
		return "", err
	}
	for _, protected := range forbidden {
		if protected == "" {
			continue
		}
		root, err := resolvedPath(protected)
		if err != nil {
			return "", err
		}
		inside, err := trialPathsOverlap(root, out)
		if err != nil {
			return "", err
		}
		if inside {
			return "", fmt.Errorf("trial --out must not overlap submissions, checkpoints or corpus artifacts")
		}
	}
	return out, nil
}

func trialPathsOverlap(left, right string) (bool, error) {
	contains := func(root, path string) (bool, error) {
		for lowerRoot, lowerPath := strings.ToLower(root), strings.ToLower(path); ; lowerPath = filepath.Dir(lowerPath) {
			if lowerPath == lowerRoot {
				return true, nil
			}
			if lowerPath == filepath.Dir(lowerPath) {
				break
			}
		}
		base, err := os.Stat(root)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect protected trial path: %w", err)
		}
		for {
			info, err := os.Stat(path)
			if err == nil && os.SameFile(base, info) {
				return true, nil
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, fmt.Errorf("inspect trial output ancestry: %w", err)
			}
			parent := filepath.Dir(path)
			if path == parent {
				return false, nil
			}
			path = parent
		}
	}
	inside, err := contains(left, right)
	if err != nil || inside {
		return inside, err
	}
	return contains(right, left)
}
