package build

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"danny.vn/trachuan/internal/embedding"
	"danny.vn/trachuan/internal/frameworks/businesskey"
	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/pdftext"
	"golang.org/x/sys/unix"
)

type runState struct {
	snapshotManifest []byte
	steps            map[string]*frozenStep
}

type RunOptions struct {
	state       *runState
	PDFRuntime  PDFRuntime
	ParserTable parse.Table
}

func Run(ctx context.Context, rel string, out io.Writer, embedder Embedder, database Database, calibration ...CalibrationOpener) error {
	return RunWithOptions(ctx, rel, out, embedder, database, RunOptions{}, calibration...)
}

func RunWithOptions(ctx context.Context, rel string, out io.Writer, embedder Embedder, database Database, options RunOptions, calibration ...CalibrationOpener) error {
	options.ParserTable = options.ParserTable.Clone()
	if err := options.ParserTable.Validate(); err != nil {
		return err
	}
	if len(calibration) > 1 {
		return fmt.Errorf("multiple calibration engine openers")
	}
	if !regexp.MustCompile(`^[0-9]{8}(_[0-9]{1,2})?$`).MatchString(rel) {
		return fmt.Errorf("invalid release id")
	}
	source, work, err := prepare()
	if err != nil {
		return err
	}
	base, err := checkedDir(filepath.Join(work, rel))
	if err != nil {
		return err
	}
	if !contained(base, work) {
		return fmt.Errorf("release directory outside work directory")
	}
	if err = makeDirectory(base); err != nil {
		return err
	}
	fd, err := unix.Open(filepath.Join(base, ".lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return hide("open release lock", err)
	}
	defer func() { _ = unix.Close(fd) }()
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return hide("release locked", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return hide("read release directory", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			if err = removeTemp(filepath.Join(base, entry.Name())); err != nil {
				return err
			}
		}
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return hide("create run id", err)
	}
	options.state = &runState{}
	request := DatabaseRequest{WorkDir: work, Rel: rel, RunID: fmt.Sprintf("%x", id)}
	for _, step := range []string{"snapshot", "parse", "relate", "chunk", "embed", "build", "evaluate"} {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s: %w", step, err)
		}
		if err := runStep(ctx, base, step, request, source, out, embedder, database, options, calibration...); err != nil {
			return fmt.Errorf("%s: %w", step, err)
		}
	}
	return nil
}

func PublishedStepVersions() map[string]int {
	versions := make(map[string]int, 5)
	for _, name := range []string{"chunk", "snapshot", "parse", "relate", "embed"} {
		versions[name] = stepRecipe(name).Version
	}
	return versions
}

func stepRecipe(step string) recipe {
	params := map[string]any{}
	version := 1
	switch step {
	case "snapshot":
		version = 2
		params["gap_kinds"] = gapKinds()
		params["public_floors"] = "sources/frameworks-floors.yaml"
		params["previous_report"] = "trachuan/previous-report.json"
	case "evaluate":
		params = map[string]any{"limit": 8, "metrics": 1, "invariant": 5, "floors": 1, "previous_report": "paired-cases/2"}
	case "build":
		version = 2
		params = databaseRecipe()
	case "parse":
		version = 4
		params["pdf_extractor_version"] = pdftext.Version
		params["pdf_layout_flags"] = pdftext.LayoutFlags()
		params["pdf_bbox_flags"] = pdftext.BBoxFlags()
		params["schema"] = "controls/1"
		params["csa_normalization"] = "trim-replace-linebreaks/1"
	case "relate":
		version = 4
		params["schema"] = "edges/1"
		params["mapping_scope"] = "cross-version/2"
		params["gap_counts"] = "kind-version-subject/1"
		params["business_key_forms"] = businesskey.Forms()
	case "embed":
		params["embedding_recipe_hash"] = embedding.DocumentRecipe1().Hash()
	case "chunk":
		params["schema"] = "chunks/1"
		params["limit"] = 512
		params["prefix_format_version"] = 1
	}
	return recipe{Name: step, Version: version, Parameters: params}
}

func runStep(ctx context.Context, base, step string, request DatabaseRequest, source string, out io.Writer, embedder Embedder, database Database, options RunOptions, calibration ...CalibrationOpener) error {
	options.ParserTable = options.ParserTable.Clone()
	if err := options.ParserTable.Validate(); err != nil {
		return err
	}
	if options.state == nil {
		options.state = &runState{}
	}
	target, err := checkedDir(filepath.Join(base, step))
	if err != nil {
		return err
	}
	if !contained(target, base) {
		return fmt.Errorf("step directory outside release directory")
	}
	info, err := os.Lstat(target)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return hide("inspect step", err)
	}
	if exists && !info.IsDir() {
		return fmt.Errorf("invalid step directory")
	}
	err = nil
	var inputs map[string]*string
	var parseInputs *snapshotParseInputs
	if step == "snapshot" && exists {
		parseInputs, err = readSnapshotParseInputs(base, options.ParserTable)
		if err == nil {
			inputs, err = snapshotInputsFrozen(parseInputs)
		}
	} else if step != "snapshot" {
		if options.state != nil && options.state.snapshotManifest != nil {
			parseInputs, err = readSnapshotWithManifest(base, options.ParserTable, options.state.snapshotManifest)
		} else {
			parseInputs, err = readSnapshotParseInputs(base, options.ParserTable)
		}
		if err == nil && step == "parse" {
			err = parseInputs.loadParseInputs()
		}
		if err == nil {
			inputs, err = stepInputs(base, step, &parseInputs.manifestHash, options.state)
		}
	}
	if err != nil {
		return err
	}
	if parseInputs != nil {
		parseInputs.state = options.state
	}
	r := stepRecipe(step)
	pdfIdentity := ""
	pdfRequired := false
	if step == "parse" {
		reg := parseInputs.registry
		needsPDF := false
		for _, v := range releaseVersions(reg) {
			pdf := options.ParserTable.NeedsPDF(v.Parser)
			needsPDF = needsPDF || pdf
		}
		if needsPDF && availablePDFRuntime(options.PDFRuntime) {
			pdfIdentity = options.PDFRuntime.RuntimeIdentity()
			if pdfIdentity == "" {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				return fmt.Errorf("PDF runtime configuration error")
			}
		}
		r, err = pdfParseRecipe(reg, pdfIdentity, options.ParserTable)
		if err != nil {
			return err
		}
		_, pdfRequired = r.Parameters["pdf_runtime_sha256"]
		if pdfRequired {
			if verifyErr := options.PDFRuntime.VerifyRuntime(ctx); verifyErr != nil || options.PDFRuntime.RuntimeIdentity() != pdfIdentity {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				return fmt.Errorf("PDF runtime verification failed")
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
		}
	}
	if step == "embed" {
		if embedder == nil {
			return fmt.Errorf("embedder unavailable")
		}
		r.Parameters["law_stage_recipe_hash"] = embedder.Recipe()
	}
	if exists {
		if step == "snapshot" {
			err = validateFrozenSnapshot(parseInputs, r, inputs)
		} else {
			err = options.state.validateStep(base, step, r, inputs)
		}
		if err != nil {
			return err
		}
		if step == "snapshot" && options.state != nil {
			options.state.snapshotManifest = append([]byte(nil), parseInputs.manifestData...)
			if _, captureErr := options.state.captureBytes(base, step, parseInputs.manifestData); captureErr != nil {
				return captureErr
			}
		}
		if step == "embed" {
			if err = validateEmbeddings(ctx, base, target, embedder, *inputs["chunk/manifest.json"], false, parseInputs); err != nil {
				return err
			}
		}
		if step == "build" {
			if err = validateBuild(base, options.state); err != nil {
				return err
			}
		}
		if _, err = fmt.Fprintf(out, "%s skipped\n", step); err != nil {
			return hide("write progress", err)
		}
		return nil
	}
	tmp, err := checkedDir(filepath.Join(base, ".tmp-"+step+"-"+request.RunID))
	if err != nil {
		return err
	}
	if !contained(tmp, base) {
		return fmt.Errorf("temporary directory outside release directory")
	}
	if err = os.Mkdir(tmp, 0700); err != nil {
		return hide("create step directory", err)
	}
	stage := newStagedOutputs(tmp)
	if parseInputs != nil {
		parseInputs.stage = stage
	}
	var opener CalibrationOpener
	if len(calibration) > 0 {
		opener = calibration[0]
	}

	switch step {
	case "snapshot":
		inputs, err = snapshotWithStage(ctx, tmp, source, options.ParserTable, stage)
	case "parse":
		err = parseStepWith(ctx, base, tmp, options.PDFRuntime, options.ParserTable, parseInputs)
	case "relate":
		err = relateStep(ctx, base, tmp, parseInputs)
	case "chunk":
		err = chunkStep(ctx, base, tmp, parseInputs)
	case "embed":
		err = embedStep(ctx, base, tmp, embedder, parseInputs)
	case "build":
		err = buildStep(ctx, base, tmp, request, embedder, database, opener, parseInputs)
	case "evaluate":
		err = evaluateStep(ctx, base, tmp, request, database, parseInputs)
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if pdfRequired {
		if verifyErr := options.PDFRuntime.VerifyRuntime(ctx); verifyErr != nil || options.PDFRuntime.RuntimeIdentity() != pdfIdentity {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("PDF runtime verification failed")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if step == "embed" {
		if err := validateEmbeddings(ctx, base, tmp, embedder, *inputs["chunk/manifest.json"], true, parseInputs); err != nil {
			return err
		}
	}
	if err := seal(tmp, target, r, inputs, stage); err != nil {
		return err
	}
	if step == "snapshot" {
		frozen, readErr := readSnapshotWithManifest(base, options.ParserTable, stage.manifest)
		if readErr != nil {
			return readErr
		}
		options.state.snapshotManifest = append([]byte(nil), frozen.manifestData...)
		if _, captureErr := options.state.captureBytes(base, step, frozen.manifestData); captureErr != nil {
			return captureErr
		}
		if captureErr := options.state.capturePublished(base, step, r, inputs); captureErr != nil {
			return captureErr
		}
	} else {
		if _, captureErr := options.state.captureBytes(base, step, stage.manifest); captureErr != nil {
			return captureErr
		}
		if captureErr := options.state.capturePublished(base, step, r, inputs); captureErr != nil {
			return captureErr
		}
	}
	if _, err := fmt.Fprintf(out, "%s complete\n", step); err != nil {
		return hide("write progress", err)
	}
	return nil
}
