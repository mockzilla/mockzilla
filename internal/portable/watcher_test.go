package portable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDirsToWatch(t *testing.T) {
	synthSpec := filepath.Join(os.TempDir(), "mockzilla-portable", "specs", "hello-world.yml")

	t.Run("spec-only service watches ConfigDir and spec dir", func(t *testing.T) {
		svc := Service{
			Name:      "petstore",
			SpecPath:  "/some/dir/openapi.yml",
			ConfigDir: "/some/dir",
		}
		dirs := dirsToWatch(svc)
		assert.ElementsMatch(t, []string{"/some/dir"}, dirs)
	})

	t.Run("bare spec file (no ConfigDir) watches the spec's dir", func(t *testing.T) {
		svc := Service{Name: "x", SpecPath: "/path/to/spec.yml"}
		dirs := dirsToWatch(svc)
		assert.ElementsMatch(t, []string{"/path/to"}, dirs)
	})

	t.Run("static-only service does not watch synthesized spec dir", func(t *testing.T) {
		svc := Service{
			Name:      "hello-world",
			SpecPath:  synthSpec,
			ConfigDir: "/src/hello-world",
			StaticDir: "/src/hello-world",
		}
		dirs := dirsToWatch(svc)
		assert.NotContains(t, dirs, filepath.Dir(synthSpec),
			"watching the synth spec dir feeds reloads back into the watcher")
		assert.ElementsMatch(t, []string{"/src/hello-world"}, dirs)
	})

	t.Run("merge service does not watch synthesized spec dir", func(t *testing.T) {
		svc := Service{
			Name:      "merged",
			SpecPath:  synthSpec,
			ConfigDir: "/src/merged",
			StaticDir: "/src/merged",
		}
		dirs := dirsToWatch(svc)
		assert.NotContains(t, dirs, filepath.Dir(synthSpec))
		assert.ElementsMatch(t, []string{"/src/merged"}, dirs)
	})
}

func TestMatchServices(t *testing.T) {
	t.Run("single-service dir routes to that service", func(t *testing.T) {
		m := map[string][]string{"/src/petstore": {"petstore"}}
		assert.Equal(t, []string{"petstore"}, matchServices("/src/petstore/openapi.yml", m))
	})

	t.Run("event in nested dir walks up to nearest watched dir", func(t *testing.T) {
		m := map[string][]string{"/src/petstore": {"petstore"}}
		assert.Equal(t,
			[]string{"petstore"},
			matchServices("/src/petstore/users/get/index.json", m))
	})

	t.Run("flat-root spec file routes to matching service only", func(t *testing.T) {
		m := map[string][]string{"/src": {"petstore", "stripe", "spoonacular"}}
		assert.Equal(t, []string{"stripe"}, matchServices("/src/stripe.yaml", m))
		assert.Equal(t, []string{"petstore"}, matchServices("/src/petstore.yml", m))
	})

	t.Run("flat-root context.yml reloads every service", func(t *testing.T) {
		m := map[string][]string{"/src": {"a", "b", "c"}}
		assert.ElementsMatch(t, []string{"a", "b", "c"}, matchServices("/src/context.yml", m))
	})

	t.Run("flat-root spec for unknown service is ignored", func(t *testing.T) {
		m := map[string][]string{"/src": {"petstore", "stripe"}}
		assert.Empty(t, matchServices("/src/unrelated.yml", m))
	})

	t.Run("event outside any watched dir returns no services", func(t *testing.T) {
		m := map[string][]string{"/src/petstore": {"petstore"}}
		assert.Empty(t, matchServices("/elsewhere/file.yml", m))
	})
}
