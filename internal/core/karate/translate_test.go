package karate

import "testing"

func TestTranslateExpressionStatusIn(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"status in [200,201]", "assert responseStatus in [200, 201]"},
		{"status in [200]", "assert responseStatus in [200]"},
		{"status in [400,412,422]", "assert responseStatus in [400, 412, 422]"},
		{"status in [200,201,202,203,204]", "assert responseStatus in [200, 201, 202, 203, 204]"},
		{"status in [200, 204]", "assert responseStatus in [200, 204]"},
	}
	for _, tc := range cases {
		got, err := TranslateExpression(tc.in)
		if err != nil {
			t.Fatalf("TranslateExpression(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("TranslateExpression(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTranslateExpressionBodyComparison(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`body.total >= 2`, `match response.total >= 2`},
		{`body.resourceType == "Patient"`, `match response.resourceType == 'Patient'`},
		{`body.issue[0].severity == "error"`, `match response.issue[0].severity == 'error'`},
		{`body.total < 5`, `match response.total < 5`},
		{`body.total > 0`, `match response.total > 0`},
		{`body.total <= 10`, `match response.total <= 10`},
		{`body.total != 3`, `match response.total != 3`},
		{`body.flag == true`, `match response.flag == true`},
		{`body.value == null`, `match response.value == null`},
		{`body.count == -3`, `match response.count == -3`},
	}
	for _, tc := range cases {
		got, err := TranslateExpression(tc.in)
		if err != nil {
			t.Fatalf("TranslateExpression(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("TranslateExpression(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTranslateExpressionHeaderComparison(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`header.ETag != ""`, `match responseHeaders['ETag'] != ''`},
		{`header.Location == "x"`, `match responseHeaders['Location'] == 'x'`},
	}
	for _, tc := range cases {
		got, err := TranslateExpression(tc.in)
		if err != nil {
			t.Fatalf("TranslateExpression(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("TranslateExpression(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTranslateExpressionVariableComparison(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`variable.Patient.id == "abc"`, `match Patient_id == 'abc'`},
		{`variable.Patient.total >= 2`, `match Patient_total >= 2`},
	}
	for _, tc := range cases {
		got, err := TranslateExpression(tc.in)
		if err != nil {
			t.Fatalf("TranslateExpression(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("TranslateExpression(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTranslateExpressionUnsupported(t *testing.T) {
	cases := []string{
		"",
		"status in []",
		"body.x == unquoted",
		"garbage expression",
	}
	for _, tc := range cases {
		if _, err := TranslateExpression(tc); err == nil {
			t.Errorf("TranslateExpression(%q) expected an error, got nil", tc)
		}
	}
}
