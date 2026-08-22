package golden

import (
	"context"
	"path/filepath"
	"testing"
)

// TestGoldenPatient runs the golden-matrix self-test against the patient
// fixture: derive -> generate -> snapshot -> run against the semantic mock,
// asserting 100% pass. It writes the .plan.json snapshot on first run and
// fails on any subsequent mismatch or any failing case.
func TestGoldenPatient(t *testing.T) {
	fx, err := LoadFixture(filepath.Join(goldenDir, "patient.json"))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	res, err := Run(context.Background(), "patient", fx)
	if err != nil {
		t.Fatalf("golden run: %v", err)
	}
	if res.Failed != 0 {
		t.Fatalf("golden run had %d failed cases: %v", res.Failed, res.FailedReqs)
	}
}
