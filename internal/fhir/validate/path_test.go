package validate

import "testing"

func TestElementSegments(t *testing.T) {
	cases := []struct {
		path string
		want []string
	}{
		{"Patient.name.given", []string{"name", "given"}},
		{"Patient.name", []string{"name"}},
		{"Patient", nil},
		{"", nil},
		{"a.b.c", []string{"b", "c"}},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			got := elementSegments(c.path)
			if len(got) != len(c.want) {
				t.Fatalf("elementSegments(%q) = %v, want %v", c.path, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("elementSegments(%q) = %v, want %v", c.path, got, c.want)
				}
			}
		})
	}
}

func TestChoiceKeyMatches(t *testing.T) {
	cases := []struct {
		key, name string
		want      bool
	}{
		{"valueString", "value", true},
		{"valueQuantity", "value", true},
		{"value", "value", false},
		{"valueString", "value[x]", true},
		{"valueQuantity", "value[x]", true},
		{"value", "value[x]", false},
		{"name", "name", false},
		{"nameText", "name", true},
		{"value2", "value", false}, // digit is not upper-case
		{"value_x", "value", false},
	}
	for _, c := range cases {
		t.Run(c.key+"/"+c.name, func(t *testing.T) {
			if got := choiceKeyMatches(c.key, c.name); got != c.want {
				t.Fatalf("choiceKeyMatches(%q, %q) = %v, want %v", c.key, c.name, got, c.want)
			}
		})
	}
}

func TestResolveLeafKey(t *testing.T) {
	parent := map[string]any{
		"valueString":   "x",
		"valueQuantity": float64(5),
		"name":          "n",
	}
	cases := []struct {
		name string
		want []string
	}{
		{"value", []string{"valueString", "valueQuantity"}},
		{"value[x]", []string{"valueString", "valueQuantity"}},
		{"name", []string{"name"}},
		{"absent", nil},
		{"other", nil},
	}
	for _, c := range cases {
		got := resolveLeafKey(parent, c.name)
		if c.want == nil {
			if got != "" {
				t.Errorf("resolveLeafKey(parent, %q) = %q, want empty", c.name, got)
			}
			continue
		}
		found := false
		for _, w := range c.want {
			if got == w {
				found = true
			}
		}
		if !found {
			t.Errorf("resolveLeafKey(parent, %q) = %q, want one of %v", c.name, got, c.want)
		}
	}
}

func TestResolvePath(t *testing.T) {
	resource := map[string]any{
		"resourceType": "Patient",
		"name": []any{
			map[string]any{"family": "Smith", "given": []any{"Alice"}},
			map[string]any{"family": "Jones"},
		},
		"deceasedBoolean": false,
		"address":         map[string]any{"city": "Sydney"},
	}
	cases := []struct {
		name       string
		path       string
		wantValues []any
		wantPres   bool
	}{
		{"root", "Patient", []any{resource}, true},
		{"name array", "Patient.name", []any{map[string]any{"family": "Smith", "given": []any{"Alice"}}, map[string]any{"family": "Jones"}}, true},
		{"name.family", "Patient.name.family", []any{"Smith", "Jones"}, true},
		{"name.given", "Patient.name.given", []any{"Alice"}, true},
		{"choice key", "Patient.deceased", []any{false}, true},
		{"choice key explicit", "Patient.deceased[x]", []any{false}, true},
		{"nested map", "Patient.address.city", []any{"Sydney"}, true},
		{"missing", "Patient.phone", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, present := resolvePath(resource, c.path)
			if present != c.wantPres {
				t.Fatalf("resolvePath(%q) present = %v, want %v", c.path, present, c.wantPres)
			}
			if len(got) != len(c.wantValues) {
				t.Fatalf("resolvePath(%q) values = %v, want %v", c.path, got, c.wantValues)
			}
		})
	}
}
