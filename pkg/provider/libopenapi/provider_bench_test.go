package libopenapi

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkNewRegistry_Petstore measures cold-start cost for a small
// spec. Useful as a regression guard rather than as an absolute number.
func BenchmarkNewRegistry_Petstore(b *testing.B) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))
	if err != nil {
		b.Skip("petstore fixture not available")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := NewRegistry(spec, Options{})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFindOperation_LazyHot measures the hot-path lookup +
// conversion cost. The first iteration triggers the lazy conversion;
// subsequent iterations should hit the per-entry sync.Once cache.
func BenchmarkFindOperation_LazyHot(b *testing.B) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))
	if err != nil {
		b.Skip("petstore fixture not available")
	}
	reg, err := NewRegistry(spec, Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = reg.FindOperation("/pets/{petId}", "GET")
	}
}
