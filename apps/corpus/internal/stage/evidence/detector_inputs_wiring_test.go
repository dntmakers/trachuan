package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/detector"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/store"
)

func detectorV1Run() stage.Run {
	return stage.Run{ID: 1, Country: "vn", Rel: testRel, Snapshot: stage.Snapshot{ExtractRecordContract: stage.ExtractRecordV1, Recipes: map[string]string{"extract": strings.Repeat("a", 64)}}}
}

func TestDetectorInputsSharedOncePerInvocation(t *testing.T) {
	calls := 0
	reader := func() ([]detectorInput, error) { calls++; return []detectorInput{}, nil }
	ds := []Detector{ocrLabelDetector{}, treeDivergenceDetector{}}
	s := New(Config{Detectors: ds})
	s.detectorReader = reader
	if _, err := s.runDetectors(context.Background(), stage.Env{}, detectorV1Run(), frozenInputs{}, nil); err != nil || calls != 1 {
		t.Fatalf("stage calls=%d err=%v", calls, err)
	}
	calls = 0
	counts := CountFindings(context.Background(), Inputs{Run: detectorV1Run(), detectorReader: reader}, ds)
	if calls != 1 || len(counts) != 2 || counts[0].NotRun != "" || counts[1].NotRun != "" {
		t.Fatalf("count calls=%d counts=%+v", calls, counts)
	}
	_ = CountFindings(context.Background(), Inputs{Run: detectorV1Run(), detectorReader: reader}, ds)
	if calls != 2 {
		t.Fatalf("loader cached across invocations: %d", calls)
	}
}

func TestDetectorInputsLegacyGuardBeforeReads(t *testing.T) {
	for _, contract := range []string{"", stage.ExtractRecordLegacy, "unknown"} {
		calls := 0
		run := detectorV1Run()
		run.Snapshot.ExtractRecordContract = contract
		in := Inputs{Run: run, Build: func() (string, string, error) { calls++; return "", "", nil }, detectorReader: func() ([]detectorInput, error) { calls++; return nil, nil }}
		counts := CountFindings(context.Background(), in, []Detector{ocrLabelDetector{}, treeDivergenceDetector{}})
		if len(counts) != 2 || counts[0].NotRun == "" || counts[1].NotRun == "" || calls != 0 {
			t.Fatalf("contract=%q counts=%+v reads=%d", contract, counts, calls)
		}
		f := newFixture(t, noneDecision())
		f.stage.cfg.Detectors = []Detector{ocrLabelDetector{}, treeDivergenceDetector{}}
		f.run.Snapshot.ExtractRecordContract = contract
		f.stage.lookup = func(context.Context, stage.Env, store.Querier, stage.Run) (artifact.Result, error) {
			calls++
			return f.build, nil
		}
		if _, err := f.stage.Plan(context.Background(), f.env, f.run); err == nil || !strings.Contains(err.Error(), "detector_incomplete") || calls != 0 {
			t.Fatalf("plan contract=%q err=%v reads=%d", contract, err, calls)
		}
		if out, err := f.stage.Process(context.Background(), f.env, f.run, stage.Item{Key: itemKey(f.run)}); err == nil || !strings.Contains(err.Error(), "detector_incomplete") || out.Artifact != nil || calls != 0 {
			t.Fatalf("process contract=%q out=%+v err=%v reads=%d", contract, out, err, calls)
		}
	}
}

func TestDetectorInputsMalformedReaderPreventsPublish(t *testing.T) {
	f := newFixture(t, noneDecision())
	f.run.Snapshot.ExtractRecordContract = stage.ExtractRecordV1
	f.run.Snapshot.Recipes = map[string]string{"extract": strings.Repeat("a", 64)}
	f.stage.cfg.Detectors = []Detector{ocrLabelDetector{}, treeDivergenceDetector{}}
	f.stage.detectorReader = func() ([]detectorInput, error) { return nil, errors.New("malformed verified input") }
	_, out, err := f.process()
	if err == nil || !strings.Contains(err.Error(), "detector_incomplete") || out.Artifact != nil {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestDetectorInputsRecipeAndLegacyBinding(t *testing.T) {
	s := New(Config{Detectors: []Detector{ocrLabelDetector{}, treeDivergenceDetector{}}})
	legacy := s.Recipe(stage.Snapshot{ExtractRecordContract: stage.ExtractRecordLegacy})
	v1 := s.Recipe(detectorV1Run().Snapshot)
	if legacy == v1 {
		t.Fatal("v1 threshold and detector recipe not bound")
	}
	specs, err := json.Marshal(s.specs())
	if err != nil {
		t.Fatal(err)
	}
	wantV1 := stage.RecipeHash(stage.EvidenceStage, "2", detector.Hash(), string(specs), serialization, "tree-transcription-divergence/1:5*min<3*max")
	wantLegacy := stage.RecipeHash(stage.EvidenceStage, "2", detector.LegacyHash(), "[]", serialization)
	if v1 != wantV1 || legacy != wantLegacy {
		t.Fatalf("v1=%s want=%s legacy=%s want=%s", v1, wantV1, legacy, wantLegacy)
	}
	f := newFixture(t, noneDecision())
	item, out, err := f.process()
	if err != nil {
		t.Fatal(err)
	}
	file := f.findings(out)
	if file.Bindings.DetectorRegistrySHA256 != "sha256:53c5f4b2ba02ce3ffa7c98ec15b4efa4ba9878047f8af85d9ed41bdfdd639534" {
		t.Fatalf("legacy binding=%+v", file.Bindings)
	}
	if _, err = f.stage.Resolve(context.Background(), f.env, f.run, item, *out.Artifact); err != nil {
		t.Fatalf("legacy resolve: %v", err)
	}
}

func TestDetectorInputsV1InheritsLegacyPredecessorBinding(t *testing.T) {
	previousReport := strings.Repeat("a", 64)
	f := newFixture(t, stage.PreviousReport{SHA256: previousReport})
	f.comparedWith(previousReport)
	prior := predecessorFile(previousReport)
	prior.Bindings.DetectorRegistrySHA256 = "sha256:" + detector.LegacyHash()
	f.predecessorRun(true, "released", prior)
	f.run.Snapshot.ExtractRecordContract = stage.ExtractRecordV1
	f.run.Snapshot.Recipes = map[string]string{"extract": strings.Repeat("a", 64)}
	f.stage.cfg.Detectors = []Detector{ocrLabelDetector{}, treeDivergenceDetector{}}
	f.stage.detectorReader = func() ([]detectorInput, error) { return []detectorInput{}, nil }
	item, out, err := f.process()
	if err != nil || out.Artifact == nil {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	file := f.findings(out)
	if file.Bindings.DetectorRegistrySHA256 != "sha256:"+detector.Hash() || file.Bindings.PredecessorRel != f.previous {
		t.Fatalf("bindings=%+v", file.Bindings)
	}
	if _, err = f.stage.Resolve(context.Background(), f.env, f.run, item, *out.Artifact); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

func TestDetectorInputsMalformedArtifactPreventsPublish(t *testing.T) {
	badText := strings.Replace(testDetectorText, `"version":1`, `"version":null`, 1)
	reader := detectorReaderSetup(t, badText)
	reader.publishBuild(t)
	f := newFixture(t, noneDecision())
	f.run.Snapshot.ExtractRecordContract = stage.ExtractRecordV1
	f.run.Snapshot.Recipes = map[string]string{"extract": reader.recipe}
	f.stage.cfg.Detectors = []Detector{ocrLabelDetector{}, treeDivergenceDetector{}}
	f.stage.detectorReader = func() ([]detectorInput, error) { return reader.read(t) }
	_, out, err := f.process()
	if err == nil || !strings.Contains(err.Error(), "detector_incomplete") || out.Artifact != nil {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}
