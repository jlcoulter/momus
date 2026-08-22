package generation

import (
	"github.com/jlcoulter/momus/internal/core/ast"
	"github.com/jlcoulter/momus/internal/core/coverage"
	coregen "github.com/jlcoulter/momus/internal/core/generation"
)

// buildRequirementAssert builds the assertion for a single requirement: an
// accept assertion for positive variants, a reject assertion for negative ones.
func buildRequirementAssert(req coverage.CoverageRequirement) *ast.Assert {
	description := "server accepts generated payload"
	expression := "status in [200,201]"
	expected := "accept"
	if isNegativeVariant(req.Variant) {
		description = "server rejects violating payload"
		expression = "status in [400,412,422]"
		expected = "reject"
	}
	return &ast.Assert{
		Description:   description,
		RequirementID: req.ID,
		Expression:    expression,
		Trace: &ast.Trace{
			ConstraintID: req.ConstraintID,
			ProfileURL:   req.ProfileURL,
			ResourceType: req.ResourceType,
			ElementPath:  req.ElementPath,
			Domain:       string(req.Domain),
			Variant:      string(req.Variant),
			Expected:     expected,
		},
	}
}

// buildSingleRequirementCase builds a single request+assert case for a
// requirement at strength 1.
func buildSingleRequirementCase(req coverage.CoverageRequirement, options coregen.BuildOptions, deps []string) ast.Node {
	requestID := coregen.RequirementResourceID(req)
	caseProfiles := coregen.OrderedProfilesForResource(req.ResourceType, req.ProfileURL, options.PreferredProfileURLsByResource)
	casePrimaryProfile := coregen.FirstProfileURL(caseProfiles)
	body, applied := options.Builder.BuildBody(req, requestID, caseProfiles, casePrimaryProfile, deps, options.Exhaustive)
	if isNegativeVariant(req.Variant) && !applied {
		return nil
	}
	return &ast.Sequence{Steps: []ast.Node{
		&ast.Request{
			Method: "PUT",
			URL:    coregen.JoinInstanceURL(coregen.BaseURLForMethod(options, "PUT"), req.ResourceType, requestID),
			Headers: map[string]string{
				"Content-Type":           "application/fhir+json",
				"X-Momus-Requirement-ID": req.ID,
			},
			Body: body,
		},
		buildRequirementAssert(req),
	}}
}
