package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

type rejectionTamper struct {
	name, want string
	mutate     func(r *Report)
}

func rejectionTampers() []rejectionTamper {
	oldProduct := func(r *Report, change func(c *Cohort)) {
		c := r.Cohorts["old_product"]
		change(&c)
		r.Cohorts["old_product"] = c
	}
	return []rejectionTamper{
		{"duplicate rejection row with a lower case_count", "previous old product counts differ from fixture", func(r *Report) {
			r.Rejections[1].CaseID = r.Rejections[0].CaseID
			oldProduct(r, func(c *Cohort) { c.CaseCount-- })
		}},
		{"duplicate rejection row", "old_product case_count differs from case records", func(r *Report) {
			r.Rejections[1].CaseID = r.Rejections[0].CaseID
		}},
		{"rejection outside the fixture", "previous rejection differs from fixture", func(r *Report) { r.Rejections[0].CaseID = "elsewhere" }},
		{"rejection that did not pass", "previous rejection differs from fixture", func(r *Report) { r.Rejections[0].Passed = false }},
		{"rejection with another reason", "previous rejection differs from fixture", func(r *Report) { r.Rejections[0].Reason = "other" }},
		{"rejection row removed", "previous old product counts differ from fixture", func(r *Report) {
			r.Rejections = r.Rejections[1:]
			oldProduct(r, func(c *Cohort) { c.CaseCount-- })
		}},
		{"rejection count lowered", "previous rejection count differs from fixture", func(r *Report) {
			oldProduct(r, func(c *Cohort) { c.RejectionCount-- })
		}},
		{"rejection count on another cohort", "previous rejection count outside old product", func(r *Report) {
			c := r.Cohorts["development"]
			c.RejectionCount = 1
			r.Cohorts["development"] = c
		}},
	}
}

func TestReleaseValidator_VersionFourCandidateChecksPreviousRejections(t *testing.T) {
	ctx := context.Background()
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	engine := func(rel string) *policyReleaseEngine {
		return &policyReleaseEngine{releaseEngine: &releaseEngine{periodEngine: periodEngine{recordedEngine: recordedEngine{fakeEngine: fakeEngine{date: date}}}, rel: rel}, version: 3}
	}
	suite, fixture := releaseSuite(t, []Case{})
	addSecondLegacyPositive(t, &suite)
	dir := t.TempDir()
	previous, _, err := Run(ctx, engine("20990101_1"), suite, Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	if previous.Version != 4 || len(previous.Rejections) != 3 {
		t.Fatalf("previous version %d with %d rejection rows", previous.Version, len(previous.Rejections))
	}
	if err = previous.WriteFiles(filepath.Join(dir, "previous")); err != nil {
		t.Fatal(err)
	}
	currentEngine := engine("20990102_1")
	current, _, err := Run(ctx, currentEngine, suite, Options{EvaluationDate: date, PreviousReport: filepath.Join(dir, "previous", "report.json")})
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 4 || !current.QualityPass {
		t.Fatalf("candidate version %d quality %t", current.Version, current.QualityPass)
	}
	if err = current.WriteFiles(filepath.Join(dir, "current")); err != nil {
		t.Fatal(err)
	}
	for i, tc := range rejectionTampers() {
		invalid := reloadReport(t, previous)
		tc.mutate(&invalid)
		path := filepath.Join(dir, fmt.Sprintf("invalid-rejections-%d", i), "report.json")
		writeReleaseFile(t, path, invalid)
		_, _, runErr := Run(ctx, currentEngine, suite, Options{EvaluationDate: date, PreviousReport: path})
		if runErr == nil || !strings.Contains(runErr.Error(), tc.want) {
			t.Fatalf("%s: Go answered %v, want %q", tc.name, runErr, tc.want)
		}
	}
	legacy, old := releaseLegacyFixtures(t, suite)
	for name, data := range map[string][]byte{"fixture.json": fixture, "legacy.json": legacy, "old-product.json": old} {
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeReleaseFile(t, filepath.Join(dir, "release.json"), releaseRow(t, currentEngine.Release()))
	command := exec.CommandContext(ctx, "python3", "-c", "import sys; from corpus_revision_support import validate_go_report_five; validate_go_report_five(sys.argv[1], 4)", dir)
	command.Dir = filepath.Join("..", "..", "..", "..", "deploy", "github")
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	if data, runErr := command.CombinedOutput(); runErr != nil {
		t.Fatalf("Python release validator and Go disagree: %v: %s", runErr, data)
	}
}

func reloadReport(t *testing.T, r Report) Report {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var copied Report
	if err = json.Unmarshal(data, &copied); err != nil {
		t.Fatal(err)
	}
	return copied
}
