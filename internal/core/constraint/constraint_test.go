package constraint

import "testing"

func TestID(t *testing.T) {
	cases := []struct {
		name  string
		parts []string
		want  string
	}{
		{"single", []string{"a"}, "a"},
		{"multiple", []string{"a", "b", "c"}, "a|b|c"},
		{"drops leading empty", []string{"", "b"}, "b"},
		{"drops trailing empty", []string{"a", ""}, "a"},
		{"drops middle empty", []string{"a", "", "c"}, "a|c"},
		{"all empty", []string{"", "", ""}, ""},
		{"no parts", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ID(c.parts...); got != c.want {
				t.Fatalf("ID(%v) = %q, want %q", c.parts, got, c.want)
			}
		})
	}
}
