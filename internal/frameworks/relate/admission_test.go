package relate

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
	"github.com/xuri/excelize/v2"
)

func TestReviewedKeysRejectDuplicatesAndNoncanonical(t *testing.T) {
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Files: []registry.File{{Path: "fixture", Role: "catalog", Format: "xlsx"}}}, {Code: "beta", Version: "1"}}}
	catalogs := map[string]*parse.Result{"alpha:1": {Nodes: []parse.Node{{Citation: "A"}}}, "beta:1": {Nodes: []parse.Node{{Citation: "B"}}}}
	valid := `{"from":{"code":"alpha","version":"1","citation":"A"},"to":{"code":"beta","version":"1","citation":"B"},"relation":"related","source":"nist-csf-references"}`
	for _, tc := range []struct {
		name, data string
		ok         bool
	}{{"valid", valid, true}, {"duplicate", valid + "\n" + valid, false}, {"extra", valid[:len(valid)-1] + `,"extra":1}`, false}, {"duplicate field", valid[:len(valid)-1] + `,"source":"nist-csf-references"}`, false}, {"unknown source", `{"from":{"code":"alpha","version":"1","citation":"A"},"to":{"code":"beta","version":"1","citation":"B"},"relation":"related","source":"unknown"}`, false}, {"noncanonical", `{"from":{"code":"ALPHA","version":"1","citation":"A"},"to":{"code":"beta","version":"1","citation":"B"},"relation":"related","source":"nist-csf-references"}`, false}} {
		t.Run(tc.name, func(t *testing.T) {
			keys, err := ReadReviewedKeys(reg, catalogs, []byte(tc.data))
			if (err == nil) != tc.ok {
				t.Fatalf("got keys %v error %v", keys, err)
			}
		})
	}
}

func TestTypedKeyOrder(t *testing.T) {
	from := Key{"alpha", "1", "A"}
	control := PublicKey{From: from, Control: &Key{"beta", "1", "B"}, Relation: "related", Source: "x"}
	law := PublicKey{From: from, Law: &LawKey{"vn", "123", "dieu-1"}, Relation: "related", Source: "x"}
	if !control.Less(law) || law.Less(control) {
		t.Fatal("control must sort before law")
	}
	if control.Kind() != "cross_version" || law.Kind() != "control_law" {
		t.Fatal("wrong kind")
	}
}

func TestAdmissionWithdrawalAndLapsedTerms(t *testing.T) {
	data := admissionWorkbook(t, "CSF 2.0", [][]string{{}, {}, {"EXAMPLE (XX): invented"}, {"", "invented (XX.YY): [Withdrawn: Moved to XX.ZZ]"}})
	file := fixtureFile(registry.File{Path: "fixture", Role: "catalog", Format: "xlsx"}, data)
	count := 2
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Access: "free", Files: []registry.File{file}, ExpectedControls: &count}}}
	catalog, err := parse.Parse("nist-csf-2-xlsx", data, "alpha", "1", &count)
	if err != nil {
		t.Fatal(err)
	}
	reader := fakeSources{{Version: VersionKey{"alpha", "1"}, File: file, Data: data}}
	got, err := Admit(reg, map[string]*parse.Result{"alpha:1": catalog}, reader, nil, nil)
	if err != nil || len(got.Keys) != 1 || got.Keys[0].Key.Relation != "moved-to" || got.Keys[0].Basis != "direct" {
		t.Fatalf("got %v %v", got, err)
	}
	reg.Frameworks[0].Access = "licensed"
	got, err = Admit(reg, map[string]*parse.Result{"alpha:1": catalog}, reader, nil, nil)
	if err != nil || len(got.Keys) != 0 {
		t.Fatalf("lapsed: %v %v", got, err)
	}
}

func TestAdmissionOLIR(t *testing.T) {
	data := admissionWorkbook(t, "Mapping", [][]string{{"Focal Document Element", "Reference Document Element", "Comments (optional)"}, {"AC-01", "A.5.2", "invented"}})
	file := fixtureFile(registry.File{Path: "fixture.xlsx", SHA256: "a", Size: 1, Role: "mapping workbook", Format: "xlsx", Parser: "nist-olir"}, data)
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "nist80053", Version: "r5", Access: "free", Files: []registry.File{file}}, {Code: "iso27001", Version: "2022"}}}
	catalogs := map[string]*parse.Result{"nist80053:r5": {Nodes: []parse.Node{{Citation: "AC-1"}}}, "iso27001:2022": {Nodes: []parse.Node{{Citation: "A.5.2"}}}}
	got, err := Admit(reg, catalogs, fakeSources([]DirectSource{{Version: VersionKey{"nist80053", "r5"}, File: file, Data: data}}), nil, nil)
	if err != nil || len(got.Keys) != 1 || got.Keys[0].Key.Relation != "related" || got.Keys[0].Key.Source != "nist-olir" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestAdmissionCSFDescribedFunction(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		bare := []string{"EXAMPLE (XX)", "", "", "", "Other: bare"}
		described := []string{"EXAMPLE (XX): invented detail", "", "", "", "Other: described"}
		if reverse {
			bare, described = described, bare
		}
		data := admissionWorkbook(t, "CSF 2.0", [][]string{{}, {}, bare, described})
		count := 1
		parsed, err := parse.Parse("nist-csf-2-xlsx", data, "alpha", "1", &count)
		if err != nil {
			t.Fatal(err)
		}
		file := fixtureFile(registry.File{Path: "fixture", Role: "catalog", Format: "xlsx"}, data)
		reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Access: "free", Files: []registry.File{file}, ExpectedControls: &count}, {Code: "beta", Version: "1", Aliases: []string{"Other"}}}}
		catalogs := map[string]*parse.Result{"alpha:1": parsed, "beta:1": {Nodes: []parse.Node{{Citation: "described"}, {Citation: "bare"}}}}
		got, err := Admit(reg, catalogs, fakeSources([]DirectSource{{Version: VersionKey{"alpha", "1"}, File: file, Data: data}}), nil, nil)
		if err != nil || len(got.Keys) != 1 || got.Keys[0].Key.From != (Key{"alpha", "1", "XX"}) || *got.Keys[0].Key.Control != (Key{"beta", "1", "described"}) || got.Keys[0].Key.Relation != "related" || got.Keys[0].Key.Source != "nist-csf-references" {
			t.Fatalf("reverse %v: %v %v", reverse, got, err)
		}
	}
}

func TestAdmissionCSFCategoryWithdrawal(t *testing.T) {
	data := admissionWorkbook(t, "CSF 2.0", [][]string{{}, {}, {"EXAMPLE (XX): invented"}, {"", "invented (XX.YY): [Withdrawn: Moved to XX.ZZ, invented narrative]"}, {"", "", "XX.YY-01: invented"}})
	count := 3
	parsed, err := parse.Parse("nist-csf-2-xlsx", data, "alpha", "1", &count)
	if err != nil {
		t.Fatal(err)
	}
	file := fixtureFile(registry.File{Path: "fixture", Role: "catalog", Format: "xlsx"}, data)
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Access: "free", Files: []registry.File{file}, ExpectedControls: &count}}}
	got, err := Admit(reg, map[string]*parse.Result{"alpha:1": parsed}, fakeSources([]DirectSource{{Version: VersionKey{"alpha", "1"}, File: file, Data: data}}), nil, nil)
	if err != nil || len(got.Keys) != 1 || got.Keys[0].Key.Control.Citation != "XX.ZZ" || got.Keys[0].Key.Relation != "moved-to" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestAdmissionTitleMutationAndOrder(t *testing.T) {
	workbook := func(refs string) []byte {
		return admissionWorkbook(t, "CSF 2.0", [][]string{{}, {}, {"EXAMPLE (XX): invented", "", "", "", refs}})
	}
	data := workbook("beta: B\nbeta: C")
	file := fixtureFile(registry.File{Path: "fixture", Role: "catalog", Format: "xlsx"}, data)
	count := 1
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Access: "free", Files: []registry.File{file}, ExpectedControls: &count}, {Code: "beta", Version: "1", Access: "licensed"}}}
	target := &parse.Result{Nodes: []parse.Node{{Citation: "B", PublisherTitle: "first"}, {Citation: "C", PublisherTitle: "second"}}}
	catalogs := map[string]*parse.Result{"beta:1": target}
	first, err := Admit(reg, catalogs, fakeSources{{Version: VersionKey{"alpha", "1"}, File: file, Data: data}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	target.Nodes[0].PublisherTitle = "changed"
	target.Nodes[1].PublisherTitle = "also changed"
	data = workbook("beta: C\nbeta: B")
	file = fixtureFile(file, data)
	reg.Frameworks[0].Files[0] = file
	second, err := Admit(reg, catalogs, fakeSources{{Version: VersionKey{"alpha", "1"}, File: file, Data: data}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first.Keys)
	b, _ := json.Marshal(second.Keys)
	if !bytes.Equal(a, b) || len(first.Keys) != 2 {
		t.Fatalf("got %s %s", a, b)
	}
}

func TestAdmissionRejectsDuplicateLawClaims(t *testing.T) {
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1"}}}
	catalogs := map[string]*parse.Result{"alpha:1": {Nodes: []parse.Node{{Citation: "A"}}}}
	law := LawLink{From: Key{"alpha", "1", "A"}, To: LawKey{"vn", "vn:1", "dieu-1"}}
	if _, err := Admit(reg, catalogs, nil, nil, []LawLink{law, law}); err == nil {
		t.Fatal("duplicate law claim accepted")
	}
}

func admissionWorkbook(t *testing.T, sheet string, rows [][]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		for j, value := range row {
			cell, err := excelize.CoordinatesToCellName(j+1, i+1)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.SetCellStr(sheet, cell, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAdmissionReviewedFallback(t *testing.T) {
	file := registry.File{Path: "fixture", Role: "catalog", Format: "xlsx"}
	v := registry.Framework{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Access: "licensed", Files: []registry.File{file}}
	reg := &registry.Registry{Frameworks: []registry.Framework{v, {Code: "beta", Version: "1"}}}
	catalog := &parse.Result{Nodes: []parse.Node{{Citation: "A"}}}
	catalogs := map[string]*parse.Result{"alpha:1": catalog, "beta:1": {Nodes: []parse.Node{{Citation: "B"}}}}
	reviewed := []byte(`{"from":{"code":"alpha","version":"1","citation":"A"},"to":{"code":"beta","version":"1","citation":"B"},"relation":"related","source":"nist-csf-references"}`)
	got, err := Admit(reg, catalogs, fakeSources([]DirectSource{{Version: VersionKey{"alpha", "1"}, File: file, Catalog: catalog}}), reviewed, nil)
	if err != nil || len(got.Keys) != 1 || got.Keys[0].Basis != "reviewed_key" {
		t.Fatalf("got %v %v", got, err)
	}
	reg.Frameworks[0].Access = "free"
	file.Path = "unregistered"
	if _, err := Admit(reg, catalogs, fakeSources([]DirectSource{{Version: VersionKey{"alpha", "1"}, File: file, Catalog: catalog}}), nil, nil); err == nil {
		t.Fatal("unregistered file accepted")
	}
}

func TestReviewedKeysRejectSourceRelationMismatch(t *testing.T) {
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1"}, {Code: "beta", Version: "1"}}}
	catalogs := map[string]*parse.Result{"alpha:1": {Nodes: []parse.Node{{Citation: "A"}}}, "beta:1": {Nodes: []parse.Node{{Citation: "B"}}}}
	row := []byte(`{"from":{"code":"alpha","version":"1","citation":"A"},"to":{"code":"beta","version":"1","citation":"B"},"relation":"equivalent","source":"nist-olir"}`)
	if _, err := ReadReviewedKeys(reg, catalogs, row); err == nil {
		t.Fatal("invalid source relation accepted")
	}
}

func TestAdmissionRejectsMalformedLawIdentities(t *testing.T) {
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1"}}}
	catalogs := map[string]*parse.Result{"alpha:1": {Nodes: []parse.Node{{Citation: "A"}}}}
	for _, target := range []LawKey{{"vn", "bad", "dieu-1"}, {"vn", "vn:1", "DIEU-1"}, {"vn", "vn:1", "dieu-1~2"}, {"xx", "vn:1", "dieu-1"}} {
		if _, err := Admit(reg, catalogs, nil, nil, []LawLink{{From: Key{"alpha", "1", "A"}, To: target}}); err == nil {
			t.Errorf("accepted %+v", target)
		}
	}
}

func TestAdmissionRejectsConflictingOLIREvidence(t *testing.T) {
	data := admissionWorkbook(t, "Mapping", [][]string{{"Focal Document Element", "Reference Document Element", "Comments (optional)"}, {"AC-1", "A.5.2", "one"}, {"AC-01", "A.5.2", "two"}})
	file := fixtureFile(registry.File{Path: "fixture", Role: "mapping workbook", Format: "xlsx", Parser: "nist-olir"}, data)
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "nist80053", Version: "r5", Access: "free", Files: []registry.File{file}}, {Code: "iso27001", Version: "2022"}}}
	catalogs := map[string]*parse.Result{"nist80053:r5": {Nodes: []parse.Node{{Citation: "AC-1"}}}, "iso27001:2022": {Nodes: []parse.Node{{Citation: "A.5.2"}}}}
	if _, err := Admit(reg, catalogs, fakeSources([]DirectSource{{Version: VersionKey{"nist80053", "r5"}, File: file, Data: data}}), nil, nil); err == nil {
		t.Fatal("conflicting evidence accepted")
	}
}

func TestAdmissionDoesNotSubstituteReviewedForApprovedSource(t *testing.T) {
	file := registry.File{Path: "fixture", Role: "catalog", Format: "xlsx"}
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Access: "free", Files: []registry.File{file}}, {Code: "beta", Version: "1"}}}
	catalogs := map[string]*parse.Result{"alpha:1": {Nodes: []parse.Node{{Citation: "A"}}}, "beta:1": {Nodes: []parse.Node{{Citation: "B"}}}}
	row := []byte(`{"from":{"code":"alpha","version":"1","citation":"A"},"to":{"code":"beta","version":"1","citation":"B"},"relation":"related","source":"nist-csf-references"}`)
	got, err := Admit(reg, catalogs, nil, row, nil)
	if err == nil {
		t.Fatalf("missing direct source accepted: %v", got)
	}
}

func TestAdmissionDropsOSCALCrossEdition(t *testing.T) {
	data := []byte(`{"catalog":{"groups":[{"id":"xx","controls":[{"id":"xx-1","links":[{"rel":"related","href":"https://example.invalid/xx-2"}]}]}]}}`)
	file := fixtureFile(registry.File{Path: "fixture", Role: "catalog", Format: "oscal-json"}, data)
	count := 2
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-oscal-800-53", Access: "free", Files: []registry.File{file}, ExpectedControls: &count}}}
	got, err := Admit(reg, nil, fakeSources{{Version: VersionKey{"alpha", "1"}, File: file, Data: data}}, nil, nil)
	if err != nil || len(got.Keys) != 0 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestAdmissionKeepsExactLocalVersion(t *testing.T) {
	data := []byte(`{"catalog":{"groups":[{"id":"xx","controls":[{"id":"xx-1","links":[{"rel":"related","href":"#xx-2"}]},{"id":"xx-2"}]}]}}`)
	file := fixtureFile(registry.File{Path: "fixture", Role: "catalog", Format: "oscal-json"}, data)
	count := 3
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "5"}, {Code: "alpha", Version: "r5", Parser: "nist-oscal-800-53", Access: "free", Files: []registry.File{file}, ExpectedControls: &count}}}
	got, err := Admit(reg, nil, fakeSources{{Version: VersionKey{"alpha", "r5"}, File: file, Data: data}}, nil, nil)
	if err != nil || len(got.Keys) != 1 || got.Keys[0].Key.Control.Version != "r5" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestReviewedKeysRejectWrongSourceOwner(t *testing.T) {
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1"}, {Code: "beta", Version: "1"}}}
	catalogs := map[string]*parse.Result{"alpha:1": {Nodes: []parse.Node{{Citation: "A"}}}, "beta:1": {Nodes: []parse.Node{{Citation: "B"}}}}
	for _, source := range []string{"nist-olir", "cis-v8.1-mappings", "nist-csf-references"} {
		row := []byte(`{"from":{"code":"alpha","version":"1","citation":"A"},"to":{"code":"beta","version":"1","citation":"B"},"relation":"related","source":"` + source + `"}`)
		if _, err := ReadReviewedKeys(reg, catalogs, row); err == nil {
			t.Errorf("accepted %s", source)
		}
	}
}

func TestAdmissionMissingRegisteredSourceFails(t *testing.T) {
	file := registry.File{Path: "fixture", Role: "catalog", Format: "xlsx"}
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Access: "free", Files: []registry.File{file}}}}
	if _, err := Admit(reg, nil, nil, nil, nil); err == nil {
		t.Fatal("omitted source accepted")
	}
}

type fakeSources []DirectSource

func (sources fakeSources) ReadSource(version registry.Framework, file registry.File) ([]byte, error) {
	for _, source := range sources {
		if source.Version == (VersionKey{version.Code, version.Version}) && source.File == file {
			return source.Data, nil
		}
	}
	return nil, fmt.Errorf("missing fixture")
}

func TestAdmissionReportsPublicDrops(t *testing.T) {
	data := admissionWorkbook(t, "CSF 2.0", [][]string{{}, {}, {"EXAMPLE (XX): invented", "", "", "", "alpha: XX\nunknown: B\nISO/IEC 27001: 2022: Annex A Controls: None"}})
	file := fixtureFile(registry.File{Path: "fixture", Role: "catalog", Format: "xlsx"}, data)
	count := 1
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Access: "free", Files: []registry.File{file}, ExpectedControls: &count}}}
	got, err := Admit(reg, nil, fakeSources{{Version: VersionKey{"alpha", "1"}, File: file, Data: data}}, nil, nil)
	if err != nil || got.MappingDropped[VersionKey{"alpha", "1"}] != 2 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestAdmissionReturnsOLIREvidence(t *testing.T) {
	data := admissionWorkbook(t, "Mapping", [][]string{{"Focal Document Element", "Reference Document Element", "Comments (optional)"}, {"AC-1", "A.5.2", "invented evidence"}})
	file := fixtureFile(registry.File{Path: "fixture", Role: "mapping workbook", Format: "xlsx", Parser: "nist-olir"}, data)
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "nist80053", Version: "r5", Access: "free", Files: []registry.File{file}}, {Code: "iso27001", Version: "2022"}}}
	catalogs := map[string]*parse.Result{"nist80053:r5": {Nodes: []parse.Node{{Citation: "AC-1"}}}, "iso27001:2022": {Nodes: []parse.Node{{Citation: "A.5.2"}}}}
	got, err := Admit(reg, catalogs, fakeSources([]DirectSource{{Version: VersionKey{"nist80053", "r5"}, File: file, Data: data}}), nil, nil)
	if err != nil || len(got.Evidence) != 1 || got.Evidence[0].Value != "invented evidence" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestAdmissionMissingCatalogRegistrationFails(t *testing.T) {
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Access: "free"}}}
	if _, err := Admit(reg, nil, fakeSources(nil), nil, nil); err == nil {
		t.Fatal("missing registered catalog accepted")
	}
}

func TestAdmissionKeepsParserRejectOutOfPublicDrops(t *testing.T) {
	data := []byte(`{"catalog":{"groups":[{"id":"xx","controls":[{"id":"xx-1","links":[{"rel":"invented","href":"#xx-2"}]},{"id":"xx-2"}]}]}}`)
	file := fixtureFile(registry.File{Path: "fixture", Role: "catalog", Format: "oscal-json"}, data)
	count := 3
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-oscal-800-53", Access: "free", Files: []registry.File{file}, ExpectedControls: &count}}}
	got, err := Admit(reg, nil, fakeSources{{Version: VersionKey{"alpha", "1"}, File: file, Data: data}}, nil, nil)
	if err != nil || got.MappingDropped[VersionKey{"alpha", "1"}] != 0 {
		t.Fatalf("parser reject became public: %v %v", got, err)
	}
}

func TestAdmissionFallbackIsScopedToOwningVersion(t *testing.T) {
	data := admissionWorkbook(t, "CSF 2.0", [][]string{{}, {}, {"EXAMPLE (XX): invented"}})
	first := fixtureFile(registry.File{Path: "a", Role: "catalog", Format: "xlsx"}, data)
	second := registry.File{Path: "b", Role: "catalog", Format: "xlsx"}
	count := 1
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Access: "free", Files: []registry.File{first}, ExpectedControls: &count}, {Code: "beta", Version: "1", Parser: "nist-csf-2-xlsx", Access: "licensed", Files: []registry.File{second}}, {Code: "gamma", Version: "1"}}}
	catalogs := map[string]*parse.Result{"beta:1": {Nodes: []parse.Node{{Citation: "B"}}}, "gamma:1": {Nodes: []parse.Node{{Citation: "C"}}}}
	row := []byte(`{"from":{"code":"beta","version":"1","citation":"B"},"to":{"code":"gamma","version":"1","citation":"C"},"relation":"related","source":"nist-csf-references"}`)
	got, err := Admit(reg, catalogs, fakeSources{{Version: VersionKey{"alpha", "1"}, File: first, Data: data}}, row, nil)
	if err != nil || len(got.Keys) != 1 || got.Keys[0].Basis != "reviewed_key" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestReviewedKeysRequireRegisteredMappingFile(t *testing.T) {
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "nist80053", Version: "r5"}, {Code: "iso27001", Version: "2022"}}}
	catalogs := map[string]*parse.Result{"nist80053:r5": {Nodes: []parse.Node{{Citation: "AC-1"}}}, "iso27001:2022": {Nodes: []parse.Node{{Citation: "A.5.2"}}}}
	row := []byte(`{"from":{"code":"nist80053","version":"r5","citation":"AC-1"},"to":{"code":"iso27001","version":"2022","citation":"A.5.2"},"relation":"related","source":"nist-olir"}`)
	if _, err := ReadReviewedKeys(reg, catalogs, row); err == nil {
		t.Fatal("unregistered mapping source accepted")
	}
}

func TestAdmissionRejectsUnknownRegisteredSource(t *testing.T) {
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Files: []registry.File{{Path: "fixture", Role: "mapping workbook", Format: "xlsx", Parser: "unknown"}}}}}
	if _, err := Admit(reg, nil, nil, nil, nil); err == nil {
		t.Fatal("unknown mapping source accepted")
	}
}

func TestAdmissionDoesNotCombineReviewedAndDirect(t *testing.T) {
	data := admissionWorkbook(t, "CSF 2.0", [][]string{{}, {}, {"EXAMPLE (XX): invented", "", "", "", "beta: B"}})
	file := fixtureFile(registry.File{Path: "fixture", Role: "catalog", Format: "xlsx"}, data)
	count := 1
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Access: "free", Files: []registry.File{file}, ExpectedControls: &count}, {Code: "beta", Version: "1"}}}
	catalogs := map[string]*parse.Result{"alpha:1": {Nodes: []parse.Node{{Citation: "XX"}}}, "beta:1": {Nodes: []parse.Node{{Citation: "B"}}}}
	row := []byte(`{"from":{"code":"alpha","version":"1","citation":"XX"},"to":{"code":"beta","version":"1","citation":"B"},"relation":"related","source":"nist-csf-references"}`)
	got, err := Admit(reg, catalogs, fakeSources{{Version: VersionKey{"alpha", "1"}, File: file, Data: data}}, row, nil)
	if err != nil || len(got.Keys) != 1 || got.Keys[0].Basis != "direct" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestAdmissionRejectsInvalidWorkbookFormat(t *testing.T) {
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "nist80053", Version: "r5", Access: "free", Files: []registry.File{{Path: "fixture", Role: "mapping workbook", Format: "pdf", Parser: "nist-olir"}}}}}
	if _, err := Admit(reg, nil, nil, nil, nil); err == nil {
		t.Fatal("invalid workbook format accepted")
	}
}

func TestAdmissionOLIRKeepsExactRegisteredTarget(t *testing.T) {
	data := admissionWorkbook(t, "Mapping", [][]string{{"Focal Document Element", "Reference Document Element"}, {"AC-1", "A.5.2"}})
	file := fixtureFile(registry.File{Path: "fixture", Role: "mapping workbook", Format: "xlsx", Parser: "nist-olir"}, data)
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "nist80053", Version: "r5", Access: "free", Files: []registry.File{file}}, {Code: "iso27001", Version: "v2022"}, {Code: "iso27001", Version: "2022"}}}
	catalogs := map[string]*parse.Result{"nist80053:r5": {Nodes: []parse.Node{{Citation: "AC-1"}}}, "iso27001:2022": {Nodes: []parse.Node{{Citation: "A.5.2"}}}}
	got, err := Admit(reg, catalogs, fakeSources([]DirectSource{{Version: VersionKey{"nist80053", "r5"}, File: file, Data: data}}), nil, nil)
	if err != nil || len(got.Keys) != 1 || got.Keys[0].Key.Control.Version != "2022" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestReviewedWithdrawalRequiresRegisteredCatalogAndCategory(t *testing.T) {
	file := registry.File{Path: "fixture", Role: "catalog", Format: "xlsx"}
	for _, tc := range []struct {
		name  string
		files []registry.File
		kind  string
		valid bool
	}{{"valid", []registry.File{file}, "category", true}, {"missing catalog", nil, "category", false}, {"function origin", []registry.File{file}, "function", false}} {
		t.Run(tc.name, func(t *testing.T) {
			reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-csf-2-xlsx", Access: "licensed", Files: tc.files}}}
			catalogs := map[string]*parse.Result{"alpha:1": {Nodes: []parse.Node{{Citation: "AA", Kind: tc.kind}, {Citation: "BB", Kind: "category"}}}}
			row := []byte(`{"from":{"code":"alpha","version":"1","citation":"AA"},"to":{"code":"alpha","version":"1","citation":"BB"},"relation":"moved-to","source":"nist-csf-withdrawals"}`)
			_, err := ReadReviewedKeys(reg, catalogs, row)
			if (err == nil) != tc.valid {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestAdmissionRejectsTamperedSourceBytes(t *testing.T) {
	file := registry.File{Path: "fixture", Role: "catalog", Format: "oscal-json", Size: 1, SHA256: "0000000000000000000000000000000000000000000000000000000000000000"}
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-oscal-800-53", Access: "free", Files: []registry.File{file}}}}
	catalog := &parse.Result{Nodes: []parse.Node{{Citation: "A"}}}
	if _, err := Admit(reg, map[string]*parse.Result{"alpha:1": catalog}, fakeSources([]DirectSource{{Version: VersionKey{"alpha", "1"}, File: file, Catalog: catalog, Data: []byte("x")}}), nil, nil); err == nil {
		t.Fatal("tampered bytes accepted")
	}
}

func fixtureFile(file registry.File, data []byte) registry.File {
	file.Size = int64(len(data))
	file.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	return file
}

func TestAdmissionUsesVerifiedCatalogForCanonicalCitations(t *testing.T) {
	data := []byte(`{"catalog":{"groups":[{"id":"xx","controls":[{"id":"xx-1","props":[{"name":"label","class":"zero-padded","value":"XX-01"}],"links":[{"rel":"related","href":"#xx-2"}]},{"id":"xx-2","props":[{"name":"label","class":"zero-padded","value":"XX-02"}]}]}]}}`)
	file := fixtureFile(registry.File{Path: "fixture", Role: "catalog", Format: "oscal-json"}, data)
	count := 3
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "alpha", Version: "1", Parser: "nist-oscal-800-53", CitationScheme: "oscal-catalog", Access: "free", Files: []registry.File{file}, ExpectedControls: &count}}}
	forged := &parse.Result{Nodes: []parse.Node{{Citation: "XX-1"}, {Citation: "XX-2"}}}
	got, err := Admit(reg, map[string]*parse.Result{"alpha:1": forged}, fakeSources{{Version: VersionKey{"alpha", "1"}, File: file, Data: data}}, nil, nil)
	if err != nil || len(got.Keys) != 1 || got.Keys[0].Key.From.Citation != "XX-01" || got.Keys[0].Key.Control.Citation != "XX-02" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestAdmissionRejectsEmptyDirectWorkbook(t *testing.T) {
	data := admissionWorkbook(t, "Mapping", [][]string{{"Focal Document Element", "Reference Document Element"}})
	file := fixtureFile(registry.File{Path: "fixture", Role: "mapping workbook", Format: "xlsx", Parser: "nist-olir"}, data)
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "nist80053", Version: "r5", Access: "free", Files: []registry.File{file}}}}
	if _, err := Admit(reg, nil, fakeSources{{Version: VersionKey{"nist80053", "r5"}, File: file, Data: data}}, nil, nil); err == nil {
		t.Fatal("empty direct workbook accepted")
	}
}

func TestOLIRParserRejectsStayPrivate(t *testing.T) {
	data := admissionWorkbook(t, "Mapping", [][]string{{"Focal Document Element", "Reference Document Element"}, {"AC-1", "A.5.2"}, {"invented malformed", "A.5.2"}})
	file := fixtureFile(registry.File{Path: "fixture", Role: "mapping workbook", Format: "xlsx", Parser: "nist-olir"}, data)
	reg := &registry.Registry{Frameworks: []registry.Framework{{Code: "nist80053", Version: "r5", Access: "free", Files: []registry.File{file}}, {Code: "iso27001", Version: "2022"}}}
	catalogs := map[string]*parse.Result{"nist80053:r5": {Nodes: []parse.Node{{Citation: "AC-1"}}}, "iso27001:2022": {Nodes: []parse.Node{{Citation: "A.5.2"}}}}
	got, err := Admit(reg, catalogs, fakeSources{{Version: VersionKey{"nist80053", "r5"}, File: file, Data: data}}, nil, nil)
	if err != nil || got.MappingDropped[VersionKey{"nist80053", "r5"}] != 0 || got.PrivateParserRejects[VersionKey{"nist80053", "r5"}] != 1 {
		t.Fatalf("parser rejects leaked: %v %v", got, err)
	}
}
