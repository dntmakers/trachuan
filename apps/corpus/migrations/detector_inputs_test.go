package migrations

import "testing"

func TestDetectorInputsMigration_Embedded(t *testing.T) {
	if _, err := FS.ReadFile("00017_detector_inputs.sql"); err != nil {
		t.Fatal(err)
	}
}
