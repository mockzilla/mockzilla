package typedef

import "github.com/mockzilla/mockzilla/v2/pkg/typedef/internal/libopenapi"

var (
	// ErrDocumentReleased reports that the registry dropped its parsed document
	// to reclaim the memory it pinned. Callers needing the document re-parse.
	ErrDocumentReleased = libopenapi.ErrDocumentReleased
)
