package libopenapi

import "errors"

var (
	// ErrDocumentReleased reports that the registry dropped its parsed document
	// to reclaim the memory it pinned.
	ErrDocumentReleased = errors.New("openapi document released")
)
