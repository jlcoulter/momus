package bulk

import (
	"context"
	"testing"

	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	"github.com/jlcoulter/momus/internal/fhir/registry"
)

func benchRealRegistry(b *testing.B) *registry.Registry {
	b.Helper()
	graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions("/tmp/pkg/package.tgz", fhirpackage.ResolveOptions{
		ConflictPolicy: fhirpackage.ConflictPolicyRootWins,
	})
	if err != nil {
		b.Fatalf("resolve: %v", err)
	}
	builder := fhirpackage.NewRegistryBuilder()
	reg, err := builder.BuildFromPackagesScoped(graph.Packages, graph.Root)
	if err != nil {
		b.Fatalf("build: %v", err)
	}
	return reg
}

func BenchmarkRealSynthesizeOne(b *testing.B) {
	reg := benchRealRegistry(b)
	gen := NewCorpusGenerator(reg, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := gen.synthesizeOne(context.Background(), "Organization", i); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRealSynthesizeOneNonExhaustive(b *testing.B) {
	reg := benchRealRegistry(b)
	gen := NewCorpusGenerator(reg, false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := gen.synthesizeOne(context.Background(), "Organization", i); err != nil {
			b.Fatal(err)
		}
	}
}
