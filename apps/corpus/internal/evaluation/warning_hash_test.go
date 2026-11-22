package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

func TestWarningFixture_HashAndCalibrationIsolation(t *testing.T) {
	files := suiteFiles(t)
	files["warning-cases.json"] = &fstest.MapFile{Data: []byte("[]\n")}
	first, err := LoadSuiteFS(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	files["warning-cases.json"] = &fstest.MapFile{Data: []byte("[ ]\n")}
	second, err := LoadSuiteFS(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	if first.WarningHash != fmt.Sprintf("%x", sha256.Sum256([]byte("[]\n"))) || first.WarningHash == second.WarningHash {
		t.Fatal("warning digest missing or unchanged")
	}
	encoded, err := json.Marshal(first.EvalHashes())
	if err != nil || !strings.Contains(string(encoded), `"Warnings":"`+first.WarningHash+`"`) {
		t.Fatalf("eval fixture hash projection = %s, %v", encoded, err)
	}
	if cacheKey(Cache{}, Options{}, first.EvalHashes()) == cacheKey(Cache{}, Options{}, second.EvalHashes()) {
		t.Fatal("cache key omitted warning digest")
	}
	firstCal, err := Calibrate(context.Background(), &cohortCalibrationEngine{}, first)
	if err != nil {
		t.Fatal(err)
	}
	secondCal, err := Calibrate(context.Background(), &cohortCalibrationEngine{}, second)
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(firstCal)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(secondCal)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("warning fixture changed calibration bytes")
	}
	first.WarningBytes[0] ^= 1
	if _, err = first.PinnedWarningCases("vn"); err == nil {
		t.Fatal("changed warning bytes accepted")
	}
	if _, _, err = Run(context.Background(), &fakeEngine{}, first, Options{}); err == nil || !strings.Contains(err.Error(), "warning fixture") {
		t.Fatalf("Run accepted changed warning bytes: %v", err)
	}
	delete(files, "warning-cases.json")
	if _, err = LoadSuiteFS(files, "."); err == nil {
		t.Fatal("missing warning fixture accepted")
	}
}
