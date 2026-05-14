// Package pack creates and reads `.mockz` archives.
//
// A `.mockz` is a gzipped tarball whose first entry is a
// `.mockzilla.json` manifest declaring the services inside. The
// manifest lets readers (extractors, info tools, cold-start paths in
// lambda) skip the full discovery walk and consume the archive
// directly. Older `.mockz` files without a manifest fall back to
// discovery on the extracted tree.
package pack

import "time"

// ManifestFilename is the name of the manifest entry inside a .mockz.
// Dot-prefixed so it never collides with user content and is skipped
// by the runtime's static scan.
const ManifestFilename = ".mockzilla.json"

// CurrentFormat is the manifest schema version emitted by this build.
// Bump when the manifest layout changes in a way readers must branch
// on. Readers refuse manifests with `format` greater than the highest
// they understand.
const CurrentFormat = 1

// Manifest is the metadata block declaring what's in a `.mockz`.
type Manifest struct {
	// Format is the schema version of this manifest. See CurrentFormat.
	Format int `json:"format"`

	// Name, when set, is a user-supplied display name for the package
	// (used in `mockzilla info` output). Omitted when not provided.
	Name string `json:"name,omitempty"`

	// Description is free-text user supplied at pack time. Omitted
	// when blank.
	Description string `json:"description,omitempty"`

	// CreatedAt is the pack-time wall clock (ISO 8601 / RFC 3339 UTC).
	CreatedAt time.Time `json:"created_at"`

	// CreatedBy is the tool identifier that produced the archive,
	// e.g. "mockzilla/2.3.0".
	CreatedBy string `json:"created_by"`

	// MinMockzillaVersion is the minimum CLI version required to load
	// this archive. Readers older than this should refuse with a clear
	// message rather than fail mysteriously. Omitted when not set.
	MinMockzillaVersion string `json:"min_mockzilla_version,omitempty"`

	// Source describes the origin of the packed tree. Auto-detected
	// from `git` when the input dir is in a repo; otherwise omitted.
	Source *Source `json:"source,omitempty"`

	// Services is the resolved service list, in registration order.
	// Each entry carries enough information for the runtime to wire
	// the service without re-walking the tree.
	Services []ServiceEntry `json:"services"`
}

// Source captures the upstream provenance of the packed content.
type Source struct {
	// Type identifies the source system. Currently only "git".
	Type string `json:"type"`

	// Remote is the upstream URL (e.g. `git@github.com:user/repo.git`).
	Remote string `json:"remote,omitempty"`

	// Ref is the symbolic ref at pack time (`refs/heads/main`,
	// `refs/tags/v1.2.3`, etc.).
	Ref string `json:"ref,omitempty"`

	// Commit is the resolved SHA the archive was packed from.
	Commit string `json:"commit,omitempty"`
}

// ServiceMode classifies how a service is composed inside the archive.
type ServiceMode string

const (
	// ModeSpec means the service is driven entirely by an OpenAPI
	// document. No static endpoint files.
	ModeSpec ServiceMode = "spec"

	// ModeStatic means the service has no spec; its endpoints come
	// from `<path>/<method?>/index.<ext>` files which the runtime
	// synthesizes a spec from at load time.
	ModeStatic ServiceMode = "static"

	// ModeMerge means the folder has both a spec and static endpoint
	// files. The runtime overlays the statics on top of the spec when
	// it loads the service.
	ModeMerge ServiceMode = "merge"
)

// ServiceEntry is a single service's place in the archive.
type ServiceEntry struct {
	// Name is the URL identity. Empty for a root-mounted service (the
	// runtime mounts it at "/"; the UI surfaces it as `.root`).
	Name string `json:"name"`

	// Mount is the resolved URL prefix the service mounts at,
	// pre-computed at pack time (folder name OR `config.yml`'s
	// `mount:`). Always starts with "/".
	Mount string `json:"mount"`

	// Dir is the in-archive path of the service folder. Always
	// forward-slash-separated. Used by the runtime to anchor file
	// reads after extraction. Empty string means the archive root
	// itself is the service folder (single-service shape).
	Dir string `json:"dir"`

	// Mode is how the service is composed (spec / static / merge).
	Mode ServiceMode `json:"mode"`

	// Files maps logical roles to in-archive paths. Any unset role is
	// omitted from the JSON.
	Files ServiceFiles `json:"files"`

	// Endpoints lists the static endpoint files for this service
	// (empty for pure-spec services). Pre-resolved at pack time so
	// the runtime can synthesise routes without re-walking.
	Endpoints []EndpointEntry `json:"endpoints,omitempty"`
}

// ServiceFiles is the set of role-to-path mappings inside the archive
// for a single service.
type ServiceFiles struct {
	// Spec is the path to the OpenAPI document inside the archive, or
	// empty when the service has no spec (static mode).
	Spec string `json:"spec,omitempty"`

	// Config is the path to the service's `config.yml`, or empty when
	// the service has no per-service config.
	Config string `json:"config,omitempty"`

	// Context is the path to the service's `context.yml`, or empty
	// when the service has no per-service context.
	Context string `json:"context,omitempty"`
}

// EndpointEntry is one static endpoint precomputed at pack time. The
// runtime uses these directly to synthesise routes instead of walking
// the file tree.
type EndpointEntry struct {
	// Method is the HTTP verb in upper case.
	Method string `json:"method"`

	// Path is the URL path relative to the service mount, including
	// any `{param}` placeholders.
	Path string `json:"path"`

	// File is the in-archive path to the response body file.
	File string `json:"file"`

	// ContentType is the MIME type derived from the file extension.
	ContentType string `json:"content_type"`
}
