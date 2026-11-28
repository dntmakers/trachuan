package build

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"golang.org/x/sys/unix"

	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
	"danny.vn/trachuan/internal/frameworks/relate"
)

func titleSources(ctx context.Context, extractor PDFExtractor, table parse.Table) (*titleCatalog, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if err := disableCoreDumps(); err != nil {
		return nil, "", err
	}
	source := os.Getenv("FRAMEWORKS_SOURCE_DIR")
	if source == "" {
		return nil, "", fmt.Errorf("source directory missing")
	}
	source, err := checkedDir(source)
	if err != nil {
		return nil, "", err
	}
	dir, err := checkedDir(filepath.Join(source, "trachuan"))
	if err != nil || !contained(dir, source) {
		return nil, "", fmt.Errorf("invalid title directory")
	}
	reg, err := readRegistry("sources/frameworks.yaml", table)
	if err != nil {
		return nil, "", err
	}
	catalog := newTitleCatalog()
	versions := releaseVersions(reg)
	if len(versions) == 0 {
		return nil, "", fmt.Errorf("no release versions")
	}
	for _, v := range versions {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		file, err := v.SelectedSourceWithTable(table)
		if err != nil {
			return nil, "", err
		}
		name, err := checkedSourceFile(source, file.Path)
		if err != nil {
			return nil, "", err
		}
		file.Path = name
		selected := registry.Registry{Frameworks: []registry.Framework{{Files: []registry.File{file}}}}
		data, err := selected.ReadAt(source, name)
		if err != nil {
			return nil, "", hide("read source", err)
		}
		result, err := ParseSourceWithTable(ctx, v, data, extractor, table)
		if err != nil {
			return nil, "", hide("parse source", err)
		}
		if err := validateLineage(result.Nodes); err != nil {
			return nil, "", err
		}
		catalog.add(v, result.Nodes)
	}
	return catalog, dir, nil
}

func ImportTitles(ctx context.Context, input string) error {
	return ImportTitlesWithExtractor(ctx, input, nil)
}

func ImportTitlesWithExtractor(ctx context.Context, input string, extractor PDFExtractor) error {
	return ImportTitlesWithOptions(ctx, input, extractor, nil)
}

func ImportTitlesWithOptions(ctx context.Context, input string, extractor PDFExtractor, table parse.Table) error {
	table = table.Clone()
	if err := table.Validate(); err != nil {
		return err
	}
	catalog, dir, err := titleSources(ctx, extractor, table)
	if err != nil {
		return err
	}
	input, err = realPath(input)
	if err != nil {
		return err
	}
	inputDir, err := checkedDir(filepath.Dir(input))
	if err != nil {
		return err
	}
	fd, err := unix.Open(filepath.Join(inputDir, filepath.Base(input)), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return hide("open title input", err)
	}
	f := os.NewFile(uintptr(fd), "title input")
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return hide("stat title input", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("title input is not a regular file")
	}
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil || !reflect.DeepEqual(header, []string{"framework_code", "version_label", "citation_norm", "title"}) {
		return fmt.Errorf("invalid input header")
	}
	rows := map[relate.Key]string{}
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		row, readErr := r.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("invalid input CSV")
		}
		k := titleKey(row[0], row[1], row[2])
		if _, exists := rows[k]; exists {
			return fmt.Errorf("duplicate title key")
		}
		rows[k] = row[3]
	}
	rows, err = catalog.validate(rows, true)
	if err != nil {
		return err
	}
	keys := make([]relate.Key, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		return titleKey(a.Code, a.Version, a.Citation).Citation < titleKey(b.Code, b.Version, b.Citation).Citation
	})
	temp, err := os.CreateTemp(dir, ".titles-*")
	if err != nil {
		return hide("create staged titles", err)
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()
	if chmodErr := temp.Chmod(0600); chmodErr != nil {
		_ = temp.Close()
		return hide("chmod staged titles", chmodErr)
	}
	w := csv.NewWriter(temp)
	err = w.Write([]string{"code", "version", "citation", "title"})
	for _, k := range keys {
		if err == nil {
			err = w.Write([]string{k.Code, k.Version, k.Citation, rows[k]})
		}
	}
	w.Flush()
	if err == nil {
		err = w.Error()
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return hide("write staged titles", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(dir, "titles.csv")); err != nil {
		return hide("replace titles", err)
	}
	return nil
}

func CheckTitles(ctx context.Context) error {
	return CheckTitlesWithExtractor(ctx, nil)
}

func CheckTitlesWithExtractor(ctx context.Context, extractor PDFExtractor) error {
	return CheckTitlesWithOptions(ctx, extractor, nil)
}

func CheckTitlesWithOptions(ctx context.Context, extractor PDFExtractor, table parse.Table) error {
	table = table.Clone()
	if err := table.Validate(); err != nil {
		return err
	}
	catalog, dir, err := titleSources(ctx, extractor, table)
	if err != nil {
		return err
	}
	path, err := realPath(filepath.Join(dir, "titles.csv"))
	if err != nil {
		return err
	}
	titleDir, err := checkedDir(filepath.Dir(path))
	if err != nil || !contained(titleDir, dir) {
		return fmt.Errorf("invalid title path")
	}
	path = filepath.Join(titleDir, filepath.Base(path))
	if _, statErr := os.Stat(path); statErr != nil {
		return hide("open titles", statErr)
	}
	rows, err := readTitles(path)
	if err != nil {
		return err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	_, err = catalog.validate(rows, true)
	return err
}
