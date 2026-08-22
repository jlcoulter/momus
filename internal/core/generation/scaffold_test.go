package generation

import "testing"

func TestStableChecksumDeterministic(t *testing.T) {
	if StableChecksum("abc") != StableChecksum("abc") {
		t.Fatal("StableChecksum is not deterministic for the same input")
	}
}

func TestStableChecksumNonNegative(t *testing.T) {
	for _, v := range []string{"", "a", "patient", "http://example.org/req/1", "\u00e9moji\U0001F600"} {
		if got := StableChecksum(v); got < 0 {
			t.Errorf("StableChecksum(%q) = %d, want non-negative", v, got)
		}
	}
}

func TestStableChecksumDistinguishesInputs(t *testing.T) {
	seen := make(map[int]string)
	for _, v := range []string{
		"patient-missing-required",
		"patient-datatype-valid",
		"patient-datatype-invalid-lexical",
		"patient-multiple-values",
		"search-Patient?name=John",
		"search-Organization?active=true",
	} {
		got := StableChecksum(v)
		if prev, ok := seen[got]; ok {
			t.Fatalf("collision between %q and %q both hashing to %d", prev, v, got)
		}
		seen[got] = v
	}
}
