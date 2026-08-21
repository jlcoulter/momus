package coverage

import "testing"

// TestIsRejectStateReadNonexistent asserts that the state-domain variant which
// asserts a strict error outcome (reading a nonexistent resource) is classified
// as reject (negative) polarity, so it renders under the "Negative" group in the
// HTML report and is treated as a reject assertion by consumers keying polarity
// off IsReject. StateDeleteNonexistent is intentionally NOT in the reject set:
// a DELETE on a nonexistent resource is portable (servers may return idempotent
// 200/204), so its assertion accepts the portable status set.
func TestIsRejectStateReadNonexistent(t *testing.T) {
	if !CoverageVariantStateReadNonexistent.IsReject() {
		t.Errorf("IsReject() = false for %q, want true (negative/error obligation)", CoverageVariantStateReadNonexistent)
	}
	if CoverageVariantStateDeleteNonexistent.IsReject() {
		t.Errorf("IsReject() = true for %q, want false (portable delete outcome)", CoverageVariantStateDeleteNonexistent)
	}
}
