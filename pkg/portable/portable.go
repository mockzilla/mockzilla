package portable

import (
	"io/fs"

	internal "github.com/mockzilla/mockzilla/v2/internal/portable"
)

// RunFS extracts an fs.FS to a temp directory and runs portable mode.
// The FS root should contain OpenAPI spec files (*.yml, *.yaml, *.json),
// and optionally: static/, app.yml, context.yml.
func RunFS(fsys fs.FS, args []string) int {
	return internal.RunFS(fsys, args)
}
