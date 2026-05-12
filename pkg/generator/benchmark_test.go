package generator

import (
	"fmt"
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/schema"
)

// buildBenchSchema builds a balanced tree of nested objects with bf^depth string leaves.
// Leaf paths look like: group_<i>_d<depth>.group_<j>_d<depth-1>...
// This exercises both the leaf-name match and the suffix-path scan in replaceValueWithMapContext.
func buildBenchSchema(depth, bf int) *schema.Schema {
	if depth == 0 {
		return &schema.Schema{Type: "string"}
	}
	props := make(map[string]*schema.Schema, bf)
	for i := 0; i < bf; i++ {
		key := fmt.Sprintf("group_%d_d%d", i, depth)
		props[key] = buildBenchSchema(depth-1, bf)
	}
	return &schema.Schema{Type: "object", Properties: props}
}

// buildBenchContext builds a service context with n keys: ~20% regex patterns,
// the rest literal filler keys. One literal key (group_0_d1) intentionally matches
// a real leaf so the hit path is exercised too.
func buildBenchContext(n int) []map[string]any {
	if n == 0 {
		return nil
	}
	ctx := make(map[string]any, n)

	regexCount := n / 5
	patterns := []string{
		".*_email_%d$",
		".*_id_%d$",
		"^prefix_%d_.*",
		"^group_0_.*_%d",
		".*_d1_%d$",
		"^xyz_%d_.*",
	}
	for i := 0; i < regexCount; i++ {
		key := fmt.Sprintf(patterns[i%len(patterns)], i)
		ctx[key] = "regex_value"
	}
	for i := regexCount; i < n; i++ {
		ctx[fmt.Sprintf("filler_field_%d", i)] = "literal_value"
	}
	if n > 0 {
		ctx["group_0_d1"] = "matched_leaf_value"
	}
	return []map[string]any{ctx}
}

// schemaVariants pick (depth, bf) so leaf counts are close to 10/50/100/500.
var schemaVariants = []struct {
	name        string
	depth, bf   int
	approxLeafN int
}{
	{"leaves_8", 3, 2, 8},      // 2^3 = 8  (~10)
	{"leaves_49", 2, 7, 49},    // 7^2 = 49 (~50)
	{"leaves_100", 2, 10, 100}, // 10^2 = 100
	{"leaves_512", 3, 8, 512},  // 8^3 = 512 (~500)
}

var ctxVariants = []int{0, 10, 50, 100, 500}

// BenchmarkResponseContextOverhead measures end-to-end Response() latency for a
// matrix of (schema leaf count) x (pre-loaded service-context size). It isolates
// the cost added by larger context: same schema, varying ctx size.
//
// Run: go test -bench=BenchmarkResponseContextOverhead -benchmem ./pkg/generator/
func BenchmarkResponseContextOverhead(b *testing.B) {
	for _, sv := range schemaVariants {
		body := buildBenchSchema(sv.depth, sv.bf)
		respSchema := &schema.ResponseSchema{
			ContentType: "application/json",
			Body:        body,
		}
		for _, ctxN := range ctxVariants {
			serviceCtx := buildBenchContext(ctxN)
			gen, err := NewGenerator(serviceCtx, nil)
			if err != nil {
				b.Fatal(err)
			}
			name := fmt.Sprintf("%s/ctx_%d", sv.name, ctxN)
			b.Run(name, func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					res := gen.Response(respSchema, nil)
					if res.IsError {
						b.Fatalf("response error: %s", string(res.Body))
					}
				}
			})
		}
	}
}
