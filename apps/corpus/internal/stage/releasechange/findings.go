package releasechange

import (
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/detector"
)

type Subject struct {
	Type   string   `json:"type"`
	Fields []string `json:"fields"`
}

func (s Subject) Validate() error {
	want := map[string]int{"document": 1, "provision": 2, "relation": 3, "claim": 3, "check_item": 4, "report": 2, "absent_document": 1}[s.Type]
	if want == 0 {
		return fmt.Errorf("unknown subject type %q", s.Type)
	}
	if len(s.Fields) != want {
		return fmt.Errorf("subject %s has %d fields, want %d", s.Type, len(s.Fields), want)
	}
	return nil
}

type FindingsBindings struct {
	Rel                       string `json:"rel"`
	DumpSHA256                string `json:"dump_sha256"`
	BuildArtifactSHA256       string `json:"build_artifact_sha256"`
	ReportSHA256              string `json:"report_sha256"`
	DetectorRegistrySHA256    string `json:"detector_registry_sha256"`
	PredecessorRel            string `json:"predecessor_rel"`
	PredecessorFindingsSHA256 string `json:"predecessor_findings_sha256"`
}

type DetectorCounts struct {
	Complete      bool              `json:"complete"`
	Severity      detector.Severity `json:"severity"`
	Observed      int               `json:"observed"`
	NotObservable int               `json:"not_observable"`
	Excluded      int               `json:"excluded"`
}

type Finding struct {
	Key           string            `json:"key"`
	Detector      string            `json:"detector"`
	Subject       Subject           `json:"subject"`
	Discriminator string            `json:"discriminator"`
	Fingerprint   string            `json:"fingerprint"`
	Semantics     int               `json:"semantics"`
	Severity      detector.Severity `json:"severity"`
	FirstRel      string            `json:"first_rel"`
	Status        string            `json:"status"`
	Carried       bool              `json:"carried"`
}

type ReviewDecision struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint"`
	Semantics   int    `json:"semantics"`
	FirstRel    string `json:"first_rel"`
}

type FindingsFile struct {
	Schema    int                       `json:"schema"`
	Bindings  FindingsBindings          `json:"bindings"`
	Detectors map[string]DetectorCounts `json:"detectors"`
	Findings  []Finding                 `json:"findings"`
	New       []string                  `json:"new"`
	Gone      []string                  `json:"gone"`
	Carried   []string                  `json:"carried"`
	Unmatched []ReviewDecision          `json:"unmatched"`
}

func DecodeFindings(data []byte) (FindingsFile, error) {
	var file FindingsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return FindingsFile{}, refuse("decode findings: %v", err)
	}
	if file.Schema != 2 {
		return FindingsFile{}, refuse("findings schema %d is not %d", file.Schema, 2)
	}
	return file, nil
}
