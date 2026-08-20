package constraint

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// Derive extracts the full constraint model from a registry. It walks every
// indexed StructureDefinition, SearchParameter, and CapabilityStatement and
// normalises the rules they define into stable Constraint values.
//
// Results are de-duplicated by identifier and returned sorted by ID.
func Derive(r *registry.Registry) ([]Constraint, error) {
	if r == nil {
		return nil, errors.New("registry is required")
	}

	all := make([]Constraint, 0)

	for _, sd := range r.StructureDefinitions() {
		if sd == nil {
			continue
		}
		for _, element := range sd.Elements {
			all = append(all, deriveElementConstraints(sd, element)...)
		}
	}

	for _, sp := range r.SearchParameters() {
		all = append(all, deriveSearchConstraints(sp)...)
	}

	for _, cs := range r.CapabilityStatements() {
		all = append(all, deriveCapabilityConstraints(cs)...)
	}

	return dedupSorted(all), nil
}

// DeriveScoped extracts the constraint model for the registry's scoped
// StructureDefinitions (the test-generation subjects), resolving each subject's
// full element tree through the registry's parent (BaseDefinition) chain via
// ResolveElements. Inherited elements are therefore attributed to the subject
// profile that inherits them, so their obligations surface on the scoped
// profile rather than being dropped. Search parameters and capability
// statements are global and are derived from every indexed resource, matching
// Derive.
//
// When no scope has been set, ScopedStructureDefinitions returns every indexed
// StructureDefinition and the element set matches Derive exactly.
func DeriveScoped(r *registry.Registry) ([]Constraint, error) {
	if r == nil {
		return nil, errors.New("registry is required")
	}

	all := make([]Constraint, 0)

	for _, sd := range r.ScopedStructureDefinitions() {
		if sd == nil {
			continue
		}
		elements, err := r.ResolveElements(sd.URL)
		if err != nil {
			// A scoped subject should always be indexed; if it is not, skip it
			// rather than failing derivation for the whole registry.
			continue
		}
		for _, element := range elements {
			all = append(all, deriveElementConstraints(sd, element)...)
		}
	}

	for _, sp := range r.SearchParameters() {
		all = append(all, deriveSearchConstraints(sp)...)
	}

	for _, cs := range r.CapabilityStatements() {
		all = append(all, deriveCapabilityConstraints(cs)...)
	}

	return dedupSorted(all), nil
}

// dedupSorted de-duplicates constraints by ID (dropping empty IDs) and returns
// them sorted by ID. Results are deterministic regardless of iteration order.
func dedupSorted(constraints []Constraint) []Constraint {
	seen := make(map[string]struct{})
	out := make([]Constraint, 0, len(constraints))
	for _, c := range constraints {
		if c.ID == "" {
			continue
		}
		if _, ok := seen[c.ID]; ok {
			continue
		}
		seen[c.ID] = struct{}{}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// deriveElementConstraints converts a single StructureDefinition element into
// its constituent constraints. Root-level elements (paths without a dot) are
// structural and carry no testable obligations.
func deriveElementConstraints(sd *model.StructureDefinition, element model.ElementDefinition) []Constraint {
	if element.Path == "" || !strings.Contains(element.Path, ".") {
		return nil
	}

	var out []Constraint
	out = append(out, cardinalityConstraint(sd, element))
	out = append(out, datatypeConstraints(sd, element)...)
	if c, ok := terminologyConstraint(sd, element); ok {
		out = append(out, c)
	}
	out = append(out, invariantConstraints(sd, element)...)
	if c, ok := referenceConstraint(sd, element); ok {
		out = append(out, c)
	}
	if c, ok := fixedConstraint(sd, element); ok {
		out = append(out, c)
	}
	if c, ok := patternConstraint(sd, element); ok {
		out = append(out, c)
	}
	return out
}

func cardinalityConstraint(sd *model.StructureDefinition, element model.ElementDefinition) Constraint {
	return Constraint{
		ID:           ID(sd.URL, element.Path, string(KindCardinality)),
		Kind:         KindCardinality,
		ProfileURL:   sd.URL,
		ResourceType: sd.Type,
		ElementPath:  element.Path,
		Min:          element.Min,
		Max:          element.Max,
	}
}

func datatypeConstraints(sd *model.StructureDefinition, element model.ElementDefinition) []Constraint {
	var out []Constraint
	for _, t := range element.Types {
		if t.Code == "" {
			continue
		}
		out = append(out, Constraint{
			ID:           ID(sd.URL, element.Path, string(KindDatatype), t.Code),
			Kind:         KindDatatype,
			ProfileURL:   sd.URL,
			ResourceType: sd.Type,
			ElementPath:  element.Path,
			Datatype:     t.Code,
		})
	}
	return out
}

func terminologyConstraint(sd *model.StructureDefinition, element model.ElementDefinition) (Constraint, bool) {
	if element.Binding == nil {
		return Constraint{}, false
	}
	return Constraint{
		ID:              ID(sd.URL, element.Path, string(KindTerminology)),
		Kind:            KindTerminology,
		ProfileURL:      sd.URL,
		ResourceType:    sd.Type,
		ElementPath:     element.Path,
		BindingStrength: element.Binding.Strength,
		ValueSet:        element.Binding.ValueSet,
	}, true
}

func invariantConstraints(sd *model.StructureDefinition, element model.ElementDefinition) []Constraint {
	var out []Constraint
	for i, inv := range element.Constraints {
		if strings.TrimSpace(inv.Expression) == "" && strings.TrimSpace(inv.Key) == "" {
			continue
		}
		key := strings.TrimSpace(inv.Key)
		if key == "" {
			key = fmt.Sprintf("inv-%d", i)
		}
		out = append(out, Constraint{
			ID:           ID(sd.URL, element.Path, string(KindInvariant), key),
			Kind:         KindInvariant,
			ProfileURL:   sd.URL,
			ResourceType: sd.Type,
			ElementPath:  element.Path,
			InvariantKey: inv.Key,
			Severity:     inv.Severity,
			Expression:   inv.Expression,
			Human:        inv.Human,
		})
	}
	return out
}

func referenceConstraint(sd *model.StructureDefinition, element model.ElementDefinition) (Constraint, bool) {
	targets := collectTargetProfiles(element)
	if len(targets) == 0 {
		return Constraint{}, false
	}
	return Constraint{
		ID:             ID(sd.URL, element.Path, string(KindReference)),
		Kind:           KindReference,
		ProfileURL:     sd.URL,
		ResourceType:   sd.Type,
		ElementPath:    element.Path,
		TargetProfiles: targets,
	}, true
}

func fixedConstraint(sd *model.StructureDefinition, element model.ElementDefinition) (Constraint, bool) {
	if element.Fixed == nil {
		return Constraint{}, false
	}
	return Constraint{
		ID:           ID(sd.URL, element.Path, string(KindFixed)),
		Kind:         KindFixed,
		ProfileURL:   sd.URL,
		ResourceType: sd.Type,
		ElementPath:  element.Path,
		Value:        element.Fixed,
	}, true
}

func patternConstraint(sd *model.StructureDefinition, element model.ElementDefinition) (Constraint, bool) {
	if element.Pattern == nil {
		return Constraint{}, false
	}
	return Constraint{
		ID:           ID(sd.URL, element.Path, string(KindPattern)),
		Kind:         KindPattern,
		ProfileURL:   sd.URL,
		ResourceType: sd.Type,
		ElementPath:  element.Path,
		Value:        element.Pattern,
	}, true
}

func collectTargetProfiles(element model.ElementDefinition) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, target := range element.TargetProfile {
		add(target)
	}
	for _, t := range element.Types {
		for _, target := range t.TargetProfile {
			add(target)
		}
	}
	sort.Strings(out)
	return out
}

func deriveSearchConstraints(sp *model.SearchParameter) []Constraint {
	var out []Constraint
	if sp == nil || strings.TrimSpace(sp.Code) == "" {
		return out
	}
	for _, base := range sp.Base {
		if strings.TrimSpace(base) == "" {
			continue
		}
		out = append(out, Constraint{
			ID:               ID(sp.URL, string(KindSearch), base, sp.Code),
			Kind:             KindSearch,
			ResourceType:     base,
			SearchName:       sp.Name,
			SearchCode:       sp.Code,
			SearchType:       sp.Type,
			SearchExpression: sp.Expression,
		})
	}
	return out
}

func deriveCapabilityConstraints(cs *model.CapabilityStatement) []Constraint {
	var out []Constraint
	if cs == nil {
		return out
	}
	for _, rest := range cs.Rest {
		if rest.Mode != "" && !strings.EqualFold(rest.Mode, "server") {
			continue
		}
		for _, res := range rest.Resource {
			if strings.TrimSpace(res.Type) == "" {
				continue
			}
			for _, inter := range res.Interaction {
				if strings.TrimSpace(inter.Code) == "" {
					continue
				}
				out = append(out, Constraint{
					ID:           ID(cs.URL, string(KindInteraction), res.Type, inter.Code),
					Kind:         KindInteraction,
					ResourceType: res.Type,
					Interaction:  inter.Code,
				})
			}
			for _, op := range res.Operation {
				name := strings.TrimSpace(op.Name)
				if name == "" {
					continue
				}
				name = strings.TrimPrefix(name, "$")
				out = append(out, Constraint{
					ID:            ID(cs.URL, string(KindOperation), res.Type, name),
					Kind:          KindOperation,
					ResourceType:  res.Type,
					OperationName: name,
				})
			}
		}
	}
	return out
}
