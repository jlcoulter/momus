package golden

import (
	"context"
	"path/filepath"
	"testing"
)

// TestGoldenAll runs the golden-matrix self-test against every reference
// fixture in testdata/golden. For each: derive -> generate -> snapshot ->
// provision -> run against the semantic mock, asserting 100% pass. It writes
// the .plan.json snapshot on first run and fails on any mismatch or failing
// case.
func TestGoldenAll(t *testing.T) {
	fixtures := []string{"patient", "observation-slice", "search-operations", "observation-invariant", "patient-date", "patient-search", "observation-value", "location-near", "observation-composite", "practitioner-role"}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			fx, err := LoadFixture(filepath.Join(goldenDir, name+".json"))
			if err != nil {
				t.Fatalf("load fixture: %v", err)
			}
			res, err := Run(context.Background(), name, fx, nil)
			if err != nil {
				t.Fatalf("golden run: %v", err)
			}
			if res.Failed != 0 {
				t.Fatalf("golden run had %d failed cases: %v", res.Failed, res.FailedReqs)
			}
		})
	}
}
