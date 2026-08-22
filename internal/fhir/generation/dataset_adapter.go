package generation

import (
	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/fhir/model"
)

// ToCoreDataset converts a FHIR Dataset into the generic core AST Dataset so it
// can be embedded in a test-plan AST. The two shapes are structurally
// identical; this is a shallow copy that keeps the AST free of FHIR types.
func ToCoreDataset(ds *model.Dataset) *ast.Dataset {
	if ds == nil {
		return nil
	}
	out := &ast.Dataset{
		Resources:     make(map[string]*ast.ResourceInstance, len(ds.Resources)),
		Relationships: make([]ast.Reference, 0, len(ds.Relationships)),
	}
	for key, inst := range ds.Resources {
		if inst == nil {
			continue
		}
		out.Resources[key] = &ast.ResourceInstance{
			LocalID:      inst.LocalID,
			ResourceType: inst.ResourceType,
			Profile:      inst.Profile,
			Resource:     inst.Resource,
			ServerID:     inst.ServerID,
			Version:      inst.Version,
		}
	}
	for _, rel := range ds.Relationships {
		out.Relationships = append(out.Relationships, ast.Reference{
			SourceID: rel.SourceID,
			Path:     rel.Path,
			TargetID: rel.TargetID,
		})
	}
	return out
}

// FromCoreDataset converts a generic core AST Dataset back into a FHIR Dataset
// for provisioning, which consumes the typed FHIR model.
func FromCoreDataset(ds *ast.Dataset) *model.Dataset {
	if ds == nil {
		return nil
	}
	out := &model.Dataset{
		Resources:     make(map[string]*model.ResourceInstance, len(ds.Resources)),
		Relationships: make([]model.Reference, 0, len(ds.Relationships)),
	}
	for key, inst := range ds.Resources {
		if inst == nil {
			continue
		}
		out.Resources[key] = &model.ResourceInstance{
			LocalID:      inst.LocalID,
			ResourceType: inst.ResourceType,
			Profile:      inst.Profile,
			Resource:     inst.Resource,
			ServerID:     inst.ServerID,
			Version:      inst.Version,
		}
	}
	for _, rel := range ds.Relationships {
		out.Relationships = append(out.Relationships, model.Reference{
			SourceID: rel.SourceID,
			Path:     rel.Path,
			TargetID: rel.TargetID,
		})
	}
	return out
}
