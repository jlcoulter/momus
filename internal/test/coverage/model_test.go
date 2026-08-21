package coverage

import "testing"

// TestIsRejectStateNonexistent asserts that the state-domain variants which
// assert error outcomes (reading/deleting a nonexistent resource) are
// classified as reject (negative) polarity, so they render under the
// "Negative" group in the HTML report and are treated as reject assertions by
// consumers keying polarity off IsReject.
func TestIsRejectStateNonexistent(t *testing.T) {
	for _, variant := range []CoverageVariant{
		CoverageVariantStateReadNonexistent,
		CoverageVariantStateDeleteNonexistent,
	} {
		if !variant.IsReject() {
			t.Errorf("IsReject() = false for %q, want true (negative/error obligation)", variant)
		}
	}
}
