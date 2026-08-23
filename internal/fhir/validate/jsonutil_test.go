package validate

import "testing"

func TestEqualJSON(t *testing.T) {
	cases := []struct {
		name string
		a    any
		b    any
		want bool
	}{
		{"equal strings", "a", "a", true},
		{"unequal strings", "a", "b", false},
		{"equal numbers", float64(1), float64(1), true},
		{"unequal numbers", float64(1), float64(2), false},
		{"number vs string", float64(1), "1", false},
		{"equal bool", true, true, true},
		{"unequal bool", true, false, false},
		{"both nil", nil, nil, true},
		{"nil vs value", nil, "x", false},
		{"equal maps", map[string]any{"a": "x"}, map[string]any{"a": "x"}, true},
		{"map different length", map[string]any{"a": "x"}, map[string]any{}, false},
		{"map missing key", map[string]any{"a": "x", "b": "y"}, map[string]any{"a": "x"}, false},
		{"map different value", map[string]any{"a": "x"}, map[string]any{"a": "y"}, false},
		{"map vs non-map", map[string]any{"a": "x"}, "a", false},
		{"equal arrays", []any{"a", "b"}, []any{"a", "b"}, true},
		{"array different length", []any{"a"}, []any{"a", "b"}, false},
		{"array different order", []any{"a", "b"}, []any{"b", "a"}, false},
		{"array vs non-array", []any{"a"}, "a", false},
		{"nested equal", map[string]any{"a": []any{map[string]any{"b": float64(1)}}}, map[string]any{"a": []any{map[string]any{"b": float64(1)}}}, true},
		{"nested unequal", map[string]any{"a": []any{map[string]any{"b": float64(1)}}}, map[string]any{"a": []any{map[string]any{"b": float64(2)}}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := equalJSON(c.a, c.b); got != c.want {
				t.Fatalf("equalJSON(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestContainsPattern(t *testing.T) {
	cases := []struct {
		name    string
		pattern any
		value   any
		want    bool
	}{
		{"scalar equal", "x", "x", true},
		{"scalar unequal", "x", "y", false},
		{"map subset", map[string]any{"a": "x"}, map[string]any{"a": "x", "b": "y"}, true},
		{"map missing key", map[string]any{"a": "x"}, map[string]any{"b": "y"}, false},
		{"map wrong value", map[string]any{"a": "x"}, map[string]any{"a": "z"}, false},
		{"map vs non-map", map[string]any{"a": "x"}, "x", false},
		{"array element present", []any{"a"}, []any{"a", "b"}, true},
		{"array element absent", []any{"c"}, []any{"a", "b"}, false},
		{"array vs non-array", []any{"a"}, "a", false},
		{"nested pattern", map[string]any{"coding": []any{map[string]any{"code": "x"}}}, map[string]any{"coding": []any{map[string]any{"code": "x", "system": "s"}}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := containsPattern(c.pattern, c.value); got != c.want {
				t.Fatalf("containsPattern(%v, %v) = %v, want %v", c.pattern, c.value, got, c.want)
			}
		})
	}
}
