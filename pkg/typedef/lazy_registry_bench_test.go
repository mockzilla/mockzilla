package typedef

import (
	"fmt"
	"strings"
	"testing"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
)

// generateBenchSpec produces a self-contained OpenAPI 3.0 document
// with `numPaths` simple GET endpoints. Used by benchmarks here so
// they don't depend on any spec from testdata/specs (which is
// gitignored) and don't vendor third-party API documents into the
// repo. Real-world specs have richer schemas, so absolute numbers
// will differ - the value of these benchmarks is relative comparison
// between parsing strategies at varying spec sizes.
func generateBenchSpec(numPaths int) []byte {
	var sb strings.Builder
	sb.WriteString("openapi: 3.0.0\n")
	sb.WriteString("info:\n  title: Bench\n  version: 1.0.0\n")
	sb.WriteString("paths:\n")
	for i := 0; i < numPaths; i++ {
		fmt.Fprintf(&sb, "  /resource%d/{id}:\n", i)
		sb.WriteString("    get:\n")
		fmt.Fprintf(&sb, "      operationId: getResource%d\n", i)
		sb.WriteString("      parameters:\n")
		sb.WriteString("        - name: id\n")
		sb.WriteString("          in: path\n")
		sb.WriteString("          required: true\n")
		sb.WriteString("          schema: {type: string}\n")
		sb.WriteString("      responses:\n")
		sb.WriteString("        '200':\n")
		sb.WriteString("          description: ok\n")
		sb.WriteString("          content:\n")
		sb.WriteString("            application/json:\n")
		sb.WriteString("              schema:\n")
		sb.WriteString("                type: object\n")
		sb.WriteString("                properties:\n")
		sb.WriteString("                  id: {type: string}\n")
		sb.WriteString("                  name: {type: string}\n")
		sb.WriteString("                  value: {type: integer}\n")
		sb.WriteString("                  tags: {type: array, items: {type: string}}\n")
	}
	return []byte(sb.String())
}

// TestLibopenapiBuildV3ModelCachesOrRebuilds verifies whether
// libopenapi.Document.BuildV3Model returns the same model pointer on
// repeated calls (cached) or a fresh model each time (rebuilds).
// Knowing this dictates whether we can cache the Document and mutate
// the model in-place per-op safely. Documented finding: it CACHES.
func TestLibopenapiBuildV3ModelCachesOrRebuilds(t *testing.T) {
	doc, err := codegen.LoadDocumentFromContents(generateBenchSpec(10))
	if err != nil {
		t.Fatalf("load doc: %v", err)
	}

	m1, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("first build: %v", err)
	}

	var firstPath string
	for path := range m1.Model.Paths.PathItems.FromOldest() {
		firstPath = path
		break
	}
	if firstPath == "" {
		t.Fatal("no paths to delete")
	}
	t.Logf("deleting path %q from m1", firstPath)
	m1.Model.Paths.PathItems.Delete(firstPath)

	m2, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("second build: %v", err)
	}

	_, stillThere := m2.Model.Paths.PathItems.Get(firstPath)
	if stillThere {
		t.Log("BuildV3Model REBUILDS - safe to cache Document and filter per-op")
	} else {
		t.Log("BuildV3Model CACHES - mutating m1 affects m2; can't naively cache Document with mutation")
	}
}

// BenchmarkParseStrategies compares the per-op parse cost of the
// current code path (CreateParseContext from bytes with filter) vs
// alternative caching strategies, across spec sizes. See
// .data/portable-libopenapi-direct.md for the architectural
// implications.
func BenchmarkParseStrategies(b *testing.B) {
	sizes := []struct {
		name     string
		numPaths int
	}{
		{"small-10", 10},
		{"medium-100", 100},
		{"large-1000", 1000},
	}

	for _, size := range sizes {
		specBytes := generateBenchSpec(size.numPaths)

		cfgFilter := codegen.Configuration{}.WithDefaults()
		cfgFilter.Filter = codegen.FilterConfig{
			Include: codegen.FilterParamsConfig{
				Paths: []string{"/resource0/{id}"},
			},
		}
		cfgNoFilter := codegen.Configuration{}.WithDefaults()

		b.Run(size.name+"/from-bytes-with-filter", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, errs := CreateParseContext(specBytes, cfgFilter, nil)
				if len(errs) > 0 {
					b.Fatalf("parse: %v", errs[0])
				}
			}
		})

		b.Run(size.name+"/from-cached-doc-no-filter", func(b *testing.B) {
			doc, err := codegen.LoadDocumentFromContents(specBytes)
			if err != nil {
				b.Fatalf("load doc: %v", err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := codegen.CreateParseContextFromDocument(doc, cfgNoFilter)
				if err != nil {
					b.Fatalf("parse: %v", err)
				}
			}
		})
	}
}
