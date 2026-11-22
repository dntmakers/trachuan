package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

func TestRun_VersionFourComparesPriorVersions(t *testing.T) {
	date := retrieval.Date{Year: 2099, Month: 1, Day: 2}
	engine := &recordedEngine{fakeEngine: fakeEngine{date: date}}
	baseline, _, err := Run(context.Background(), engine, developmentSuite(), Options{EvaluationDate: date})
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{2, 3, 4} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			prior := baseline
			prior.Version, prior.Country, prior.Rel = version, "vn", "20990101_1"
			data, err := json.Marshal(prior)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ParseDecisionReport(data); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "report.json")
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			current, _, err := Run(context.Background(), engine, developmentSuite(), Options{EvaluationDate: date, PreviousReport: path})
			if err != nil {
				t.Fatal(err)
			}
			if current.Version != 4 {
				t.Fatalf("version = %d", current.Version)
			}
			sum := fmt.Sprintf("%x", sha256.Sum256(data))
			previous, err := current.ComparedRelease()
			if err != nil || previous == nil || previous.ReportSHA256 != sum {
				t.Fatalf("previous = %+v, %v", previous, err)
			}
			if len(current.NonRegression) == 0 || current.NonRegression[0].Status == "no_previous" {
				t.Fatal("non-regression not compared")
			}
		})
	}
}
