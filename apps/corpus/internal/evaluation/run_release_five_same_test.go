package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

func TestReleaseValidator_RejectsInvalidGoVersionFivePrevious(t *testing.T) {
	ctx := context.Background()
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	engine := func(rel string) *policyReleaseEngine {
		return &policyReleaseEngine{releaseEngine: &releaseEngine{periodEngine: periodEngine{recordedEngine: recordedEngine{fakeEngine: fakeEngine{date: date}}}, rel: rel}, version: 4}
	}
	suite, fixture := releaseSuite(t, []Case{})
	addSecondLegacyPositive(t, &suite)
	dir := t.TempDir()
	previous, _, err := Run(ctx, engine("20990101_1"), suite, Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	if err = previous.WriteFiles(filepath.Join(dir, "previous")); err != nil {
		t.Fatal(err)
	}
	currentEngine := engine("20990102_1")
	current, _, err := Run(ctx, currentEngine, suite, Options{EvaluationDate: date, PreviousReport: filepath.Join(dir, "previous", "report.json")})
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 5 || !current.QualityPass {
		t.Fatalf("version %d quality %t", current.Version, current.QualityPass)
	}
	if err = current.WriteFiles(filepath.Join(dir, "current")); err != nil {
		t.Fatal(err)
	}
	for i, tc := range previousRound3TamperCases() {
		data, marshalErr := json.Marshal(previous)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		var invalid Report
		if err = json.Unmarshal(data, &invalid); err != nil {
			t.Fatal(err)
		}
		tc.mutate(&invalid)
		path := filepath.Join(dir, fmt.Sprintf("invalid-%d", i), "report.json")
		writeReleaseFile(t, path, invalid)
		if _, _, runErr := Run(ctx, currentEngine, suite, Options{EvaluationDate: date, PreviousReport: path}); runErr == nil {
			t.Fatalf("Go accepted %s", tc.name)
		}
	}
	legacy, old := releaseLegacyFixtures(t, suite)
	for name, data := range map[string][]byte{"fixture.json": fixture, "legacy.json": legacy, "old-product.json": old} {
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeReleaseFile(t, filepath.Join(dir, "release.json"), releaseRow(t, currentEngine.Release()))
	command := exec.CommandContext(ctx, "python3", "-c", "import sys; from corpus_revision_support import validate_go_report_five; validate_go_report_five(sys.argv[1])", dir)
	command.Dir = filepath.Join("..", "..", "..", "..", "deploy", "github")
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	if data, runErr := command.CombinedOutput(); runErr != nil {
		t.Fatalf("Python release validator refused Go reports: %v: %s", runErr, data)
	}
}
