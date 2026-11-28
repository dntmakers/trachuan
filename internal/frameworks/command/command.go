package command

import (
	"context"
	"fmt"
	"io"
	"os"

	"danny.vn/trachuan/internal/frameworks/build"
	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
	"danny.vn/trachuan/internal/frameworks/relate"
)

func Run(args []string, out io.Writer) error {
	return RunWithExtractor(context.Background(), args, out, build.NewLazyPDFRuntime(context.Background()))
}

func RunWithExtractor(ctx context.Context, args []string, out io.Writer, extractor build.PDFExtractor) error {
	return RunWithOptions(ctx, args, out, extractor, nil)
}

func RunWithOptions(ctx context.Context, args []string, out io.Writer, extractor build.PDFExtractor, table parse.Table) error {
	table = table.Clone()
	if err := table.Validate(); err != nil {
		return fmt.Errorf("frameworks command failed")
	}
	if len(args) != 2 && (len(args) != 1 || args[0] != "edges") {
		return fmt.Errorf("usage: frameworks <code> <version> | frameworks edges")
	}
	f, err := os.Open("sources/frameworks.yaml")
	if err != nil {
		return fmt.Errorf("frameworks command failed")
	}
	defer func() { _ = f.Close() }()
	reg, err := registry.ParseWithTable(f, table)
	if err != nil {
		return fmt.Errorf("frameworks command failed")
	}

	if len(args) == 1 {
		licensed := false
		for _, v := range reg.Frameworks {
			if v.Access != "licensed" {
				continue
			}
			if v.Parser != "" {
				licensed = true
			}
			for _, file := range v.Files {
				if file.Role == "mapping workbook" {
					licensed = true
				}
			}
		}
		if licensed {
			if err := edgeStatsWith(ctx, reg, io.Discard, extractor, table); err != nil {
				return fmt.Errorf("frameworks command failed")
			}
			return nil
		}
		return edgeStatsWith(ctx, reg, out, extractor, table)
	}
	targets := map[string]string{}
	for _, framework := range reg.Frameworks {
		targets[framework.Code] = framework.Code
		for _, alias := range framework.Aliases {
			targets[alias] = framework.Code
		}
	}
	for _, v := range reg.Frameworks {
		if v.Code != args[0] || v.Version != args[1] {
			continue
		}
		file, err := v.SelectedSourceWithTable(table)
		if err != nil {
			if v.Access == "licensed" {
				return fmt.Errorf("frameworks command failed")
			}
			return err
		}
		data, err := reg.Read(file.Path)
		if err != nil {
			if v.Access == "licensed" {
				return fmt.Errorf("frameworks command failed")
			}
			return fmt.Errorf("read catalog: %w", err)
		}
		result, err := build.ParseSourceWithTable(ctx, v, data, extractor, table)
		if v.Access == "licensed" {
			if err != nil {
				return fmt.Errorf("frameworks command failed")
			}
			if _, writeErr := fmt.Fprintf(out, "controls: %d\n", *v.ExpectedControls); writeErr != nil {
				return fmt.Errorf("frameworks command failed")
			}
			return nil
		}
		if result != nil {
			if reportErr := parse.Report(out, result, targets); reportErr != nil {
				return fmt.Errorf("write statistics: %w", reportErr)
			}
		}
		return err
	}
	return fmt.Errorf("frameworks command failed")
}

func edgeStatsWith(ctx context.Context, reg *registry.Registry, out io.Writer, extractor build.PDFExtractor, table parse.Table) error {
	catalogs := map[string]*parse.Result{}
	for _, v := range reg.Frameworks {
		if v.Parser == "" {
			continue
		}
		file, err := v.SelectedSourceWithTable(table)
		if err != nil {
			return err
		}
		data, err := reg.Read(file.Path)
		if err != nil {
			return fmt.Errorf("read catalog: %w", err)
		}
		result, err := build.ParseSourceWithTable(ctx, v, data, extractor, table)
		if err != nil {
			return fmt.Errorf("parse catalog: %w", err)
		}
		catalogs[v.Code+":"+v.Version] = result
	}
	var inputs []relate.Input
	dropped := 0
	for _, v := range reg.Frameworks {
		for _, file := range v.Files {
			if file.Role != "mapping workbook" {
				continue
			}
			data, err := reg.Read(file.Path)
			if err != nil {
				return fmt.Errorf("read mapping workbook: %w", err)
			}
			var nodes []parse.Node
			if result := catalogs[v.Code+":"+v.Version]; result != nil {
				nodes = result.Nodes
			}
			rows, count, err := relate.Workbook(file.Parser, data, nodes)
			if err != nil {
				return fmt.Errorf("parse mapping workbook: %w", err)
			}
			inputs = append(inputs, rows...)
			dropped += count
		}
	}
	edges, stats, err := relate.Build(reg, catalogs, inputs)
	if err != nil {
		return fmt.Errorf("build edges: %w", err)
	}
	stats.Dropped += dropped
	return relate.Report(out, edges, stats, reg)
}
