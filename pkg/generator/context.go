package generator

import (
	"github.com/mockzilla/mockzilla/v2/internal/contexts"
	"github.com/mockzilla/mockzilla/v2/resources"
)

// LoadServiceContext loads the service context and combines it with the default contexts.
// It returns a flattened array of context maps in a specific order:
//   - [0]: service - The service-specific context loaded from serviceCtx YAML
//   - [1]: common - Common context with patterns like _id$, _email$, etc. (from resources/contexts/common.yml)
//   - [2]: fake - Fake data generators for testing (from resources/contexts/fake.yml)
//   - [3]: words - Common words for realistic-looking data (from resources/contexts/words.yml)
//
// The map keys ("service", "common", "fake", "words") correspond to context namespaces
// that are loaded from embedded YAML files in the resources/contexts directory.
// These namespaces are used during the Load() call to organize and resolve context data,
// but are discarded in the returned array since only the values are needed for replacement.
func LoadServiceContext(serviceCtx []byte, defaultContexts []map[string]map[string]any) []map[string]any {
	combinedCtx := contexts.Load(
		map[string][]byte{"service": serviceCtx},
		defaultContexts)

	// names not needed anymore
	return []map[string]any{
		combinedCtx["service"],
		combinedCtx["common"],
		combinedCtx["fake"],
		combinedCtx["words"],
	}
}

// LoadDefaultContexts loads the built-in replacement contexts (common, fake, words).
// This allows standalone usage without an api.Router.
func LoadDefaultContexts() []map[string]map[string]any {
	ctxs := contexts.Load(map[string][]byte{
		"common": resources.CommonContextYAMLContents,
		"fake":   resources.FakeContextYAMLContents,
		"words":  resources.WordsContextYAMLContents,
	}, nil)
	return []map[string]map[string]any{
		{"common": ctxs["common"]},
		{"fake": ctxs["fake"]},
		{"words": ctxs["words"]},
	}
}
