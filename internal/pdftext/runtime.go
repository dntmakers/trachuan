package pdftext

import (
	"bufio"
	"debug/macho"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func (e *Extractor) RuntimeIdentity() string { return e.runtime.identity }

func (e *Extractor) VerifyRuntime() error {
	if e == nil || e.runtime.snapshot == nil || e.platform == nil || e.runtime.identity == "" {
		return ErrConfiguration
	}
	if err := checkRuntimeSnapshot(e.runtime, e.runtime.snapshot); err != nil {
		return ErrConfiguration
	}
	platform, err := e.platform(e.runtime.cacheDirs)
	if err != nil {
		return ErrConfiguration
	}
	current := e.runtime
	current.platform = platform
	identity, err := runtimeIdentity(current, "pdftotext version "+Version+"\n")
	if err != nil || identity != e.runtime.identity {
		return ErrConfiguration
	}
	if err := checkRuntimeSnapshot(e.runtime, e.runtime.snapshot); err != nil {
		return ErrConfiguration
	}
	return nil
}

func binaryRpaths(path, executable string) ([]string, error) {
	file, err := macho.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var paths []string
	for _, load := range file.Loads {
		rpath, ok := load.(*macho.Rpath)
		if !ok {
			continue
		}
		value := rpath.Path
		switch {
		case strings.HasPrefix(value, "@loader_path/"):
			value = filepath.Join(filepath.Dir(path), strings.TrimPrefix(value, "@loader_path/"))
		case strings.HasPrefix(value, "@executable_path/"):
			value = filepath.Join(filepath.Dir(executable), strings.TrimPrefix(value, "@executable_path/"))
		case !filepath.IsAbs(value):
			return nil, errors.New("unsupported runpath")
		}
		paths = append(paths, value)
	}
	return paths, nil
}

func resolveRuntime(executable string) (runtimeSet, error) {
	var set runtimeSet
	seen := map[string]bool{}
	aliases := map[string]bool{}
	addMetadata := func(path string) {
		for {
			aliases[path] = true
			parent := filepath.Dir(path)
			if parent == path {
				break
			}
			path = parent
		}
	}
	var visit func(string, []string) error
	visit = func(path string, inherited []string) error {
		addMetadata(path)
		real, links, directories, err := traceRuntimePath(path)
		if err != nil {
			return err
		}
		for _, dir := range directories {
			addMetadata(dir)
		}
		for _, link := range links {
			addMetadata(link)
			set.files = append(set.files, link)
		}
		addMetadata(real)
		set.files = append(set.files, path)
		if seen[real] {
			return nil
		}
		seen[real] = true
		parts := strings.Split(strings.TrimPrefix(real, "/opt/homebrew/Cellar/"), "/")
		if !strings.HasPrefix(real, "/opt/homebrew/Cellar/") || len(parts) < 3 {
			return errors.New("invalid runtime path")
		}
		set.files = append(set.files, real)
		rpaths, err := binaryRpaths(real, executable)
		if err != nil {
			return err
		}
		rpaths = append(rpaths, inherited...)
		cmd := exec.Command("/usr/bin/otool", "-L", real)
		cmd.Env = []string{"LC_ALL=C"}
		data, err := cmd.Output()
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		if !scanner.Scan() {
			return errors.New("invalid library list")
		}
		for scanner.Scan() {
			lib, _, ok := strings.Cut(strings.TrimSpace(scanner.Text()), " (compatibility version ")
			if !ok {
				return errors.New("invalid library record")
			}
			if strings.HasPrefix(lib, "/usr/lib/") || strings.HasPrefix(lib, "/System/") {
				set.systemFiles = append(set.systemFiles, lib)
				addMetadata(lib)
				continue
			}
			var dependency string
			switch {
			case strings.HasPrefix(lib, "@rpath/"):
				name := strings.TrimPrefix(lib, "@rpath/")
				for _, rpath := range rpaths {
					candidate := filepath.Join(rpath, name)
					resolved, err := filepath.EvalSymlinks(candidate)
					if err == nil {
						if dependency != "" {
							existing, resolveErr := filepath.EvalSymlinks(dependency)
							if resolveErr != nil || existing != resolved {
								return errors.New("ambiguous library")
							}
						}
						dependency = candidate
					}
				}
			case strings.HasPrefix(lib, "@loader_path/"):
				dependency = filepath.Join(filepath.Dir(real), strings.TrimPrefix(lib, "@loader_path/"))
			case strings.HasPrefix(lib, "@executable_path/"):
				dependency = filepath.Join(filepath.Dir(executable), strings.TrimPrefix(lib, "@executable_path/"))
			case filepath.IsAbs(lib):
				dependency = lib
			}
			if dependency == "" {
				return errors.New("unresolved library")
			}
			if err := visit(dependency, rpaths); err != nil {
				return err
			}
		}
		return scanner.Err()
	}
	if err := visit(executable, nil); err != nil {
		return runtimeSet{}, err
	}
	poppler := filepath.Dir(filepath.Dir(executable))
	data, err := runtimeDataPaths("/opt/homebrew/Cellar", poppler)
	if err != nil {
		return runtimeSet{}, err
	}
	set.files = append(set.files, data.files...)
	set.dataDirs = append(set.dataDirs, data.dataDirs...)
	for _, path := range data.metadata {
		addMetadata(path)
	}
	set.systemFiles = append(set.systemFiles, "/usr/lib/dyld")
	addMetadata("/usr/lib/dyld")
	for _, path := range []string{"/System/Library/dyld", "/System/Volumes/Preboot/Cryptexes/OS/System/Library/dyld", "/private/var/db/dyld"} {
		if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
			continue
		} else if statErr != nil {
			return runtimeSet{}, statErr
		}
		real, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil {
			return runtimeSet{}, resolveErr
		}
		set.systemDirs = append(set.systemDirs, real)
		set.cacheDirs = append(set.cacheDirs, real)
		addMetadata(path)
		addMetadata(real)
	}
	for _, path := range set.files {
		addMetadata(path)
	}
	for _, path := range set.dataDirs {
		addMetadata(path)
	}
	for path := range aliases {
		set.metadata = append(set.metadata, path)
	}
	sort.Strings(set.files)
	sort.Strings(set.systemFiles)
	sort.Strings(set.dataDirs)
	sort.Strings(set.systemDirs)
	sort.Strings(set.metadata)
	set.snapshot, err = snapshotRuntime(set)
	if err != nil {
		return runtimeSet{}, err
	}
	set.platform, err = readPlatformIdentity(set.cacheDirs)
	if err != nil {
		return runtimeSet{}, err
	}
	set.identity, err = runtimeIdentity(set, "pdftotext version "+Version+"\n")
	if err != nil {
		return runtimeSet{}, err
	}
	if err = checkRuntimeSnapshot(set, set.snapshot); err != nil {
		return runtimeSet{}, err
	}
	return set, nil
}
