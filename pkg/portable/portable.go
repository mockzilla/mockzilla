package portable

import (
	"io/fs"

	internal "github.com/mockzilla/mockzilla/v2/internal/portable"
)

// RunFS extracts an fs.FS to a temp directory and runs portable mode.
// The FS root must follow the per-service layout:
//
//	services/<name>/openapi.yml         (or any *.{yml,yaml,json} spec)
//	services/<name>/config.yml          (optional: latency/errors/mount/...)
//	services/<name>/context.yml         (optional: flat replacement values)
//	services/<name>/static/...          (optional: pre-canned responses)
//	app.yml                             (optional: global app settings)
//
// The flat single-service layout (no services/ wrapper, openapi at root)
// is also accepted; the service is named after the FS root's basename.
func RunFS(fsys fs.FS, args []string) int {
	return internal.RunFS(fsys, args)
}
