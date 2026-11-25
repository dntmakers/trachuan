package publicsummary

import "testing"

func TestAdmission_SharedAndSampleCaps(t *testing.T) {
	gate := NewAdmission()
	for range 2 {
		if !gate.TrySample() {
			t.Fatal("sample slots should be available")
		}
	}
	if gate.TrySample() {
		t.Fatal("sample cap should reject a third request")
	}
	for range 6 {
		if !gate.TrySummary() {
			t.Fatal("summary should use remaining shared capacity")
		}
	}
	if gate.TrySummary() {
		t.Fatal("shared gate should reject the ninth request")
	}
	gate.DoneSample()
	if !gate.TrySummary() {
		t.Fatal("released sample slot should be available to summary")
	}
	if gate.TrySample() {
		t.Fatal("sample must also respect shared capacity")
	}
	gate.DoneSummary()
	if !gate.TrySample() {
		t.Fatal("released shared slot should be available to sample")
	}
}
