package karate

import (
	"fmt"
	"sort"
	"strings"
)

// Render renders a FeatureFile as Gherkin text.
func Render(f FeatureFile) string {
	var b strings.Builder

	// Feature-level tags: the union of all scenario tags, deduplicated.
	featureTags := make(map[string]bool)
	for _, sc := range f.Scenarios {
		for _, t := range sc.Tags {
			featureTags[t] = true
		}
	}
	featureTagList := make([]string, 0, len(featureTags))
	for t := range featureTags {
		featureTagList = append(featureTagList, t)
	}
	sort.Strings(featureTagList)
	for _, t := range featureTagList {
		b.WriteString(t)
		b.WriteByte('\n')
	}

	b.WriteString("Feature: " + f.Name + " conformance\n")

	if len(f.Background) > 0 {
		b.WriteString("\n  Background:\n")
		writeSteps(&b, f.Background)
	}

	for _, sc := range f.Scenarios {
		b.WriteString("\n")
		for _, t := range sc.Tags {
			b.WriteString("  " + t + "\n")
		}
		b.WriteString("  Scenario: " + sc.Name + "\n")
		writeSteps(&b, sc.Steps)
	}

	return b.String()
}

// writeSteps writes a list of steps at two-space indentation, emitting any
// doc-string bodies after their step line.
func writeSteps(b *strings.Builder, steps []Step) {
	for _, s := range steps {
		indentedText := indentStep(s.Text)
		if s.DocString == "" {
			b.WriteString("    " + s.Keyword + " " + indentedText + "\n")
			continue
		}
		b.WriteString("    " + s.Keyword + " " + indentedText + "\n")
		b.WriteString("      \"\"\"\n")
		b.WriteString(indentDocString(s.DocString) + "\n")
		b.WriteString("      \"\"\"\n")
	}
}

// indentStep normalizes the step text so it reads naturally on a Gherkin line.
func indentStep(text string) string {
	return strings.TrimSpace(text)
}

// indentDocString indents a JSON doc string body by six spaces so it aligns
// under the opening triple-quote.
func indentDocString(body string) string {
	lines := strings.Split(body, "\n")
	for i := range lines {
		lines[i] = "      " + lines[i]
	}
	return strings.Join(lines, "\n")
}

// RenderAll renders a list of feature files into a filename->content map. The
// filename is "<ResourceType>.feature".
func RenderAll(files []FeatureFile) map[string]string {
	out := make(map[string]string, len(files))
	for _, f := range files {
		out[filenameFor(f)] = Render(f)
	}
	return out
}

// filenameFor returns the on-disk filename for a feature file.
func filenameFor(f FeatureFile) string {
	return fmt.Sprintf("%s.feature", f.Name)
}
