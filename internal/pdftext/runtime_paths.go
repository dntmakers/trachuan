package pdftext

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func traceRuntimePath(path string) (string, []string, []string, error) {
	if !filepath.IsAbs(path) {
		return "", nil, nil, errors.New("runtime path is not absolute")
	}
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator))
	current := string(filepath.Separator)
	var files, directories []string
	links := 0
	for len(parts) > 0 {
		part := parts[0]
		parts = parts[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			current = filepath.Dir(current)
			continue
		}
		next := filepath.Join(current, part)
		info, err := os.Lstat(next)
		if err != nil {
			return "", nil, nil, err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			current = next
			continue
		}
		links++
		if links > 40 {
			return "", nil, nil, errors.New("too many runtime symlinks")
		}
		if len(parts) == 0 {
			files = append(files, next)
		} else {
			directories = append(directories, next)
		}
		target, err := os.Readlink(next)
		if err != nil {
			return "", nil, nil, err
		}
		if filepath.IsAbs(target) {
			current = string(filepath.Separator)
		}
		parts = append(strings.Split(target, string(filepath.Separator)), parts...)
	}
	return current, files, directories, nil
}
