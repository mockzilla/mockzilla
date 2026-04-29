package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/internal/files"
	"github.com/mockzilla/mockzilla/v2/pkg/typedef"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	"go.yaml.in/yaml/v4"
)

var (
	flagConfig                string
	flagOutput                string
	flagKeepOptional          int
	flagMinOptionalProperties int
	flagMaxOptionalProperties int
	flagPrintUsage            bool
)

const cmdPath = "github.com/mockzilla/mockzilla/v2/cmd/gen/simplify"

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: go run %s [options] <path-to-spec>\n\n", cmdPath)
		fmt.Fprintf(os.Stderr, "Simplifies an OpenAPI spec by removing or reducing union types (anyOf/oneOf).\n\n")
		fmt.Fprintf(os.Stderr, "The command:\n")
		fmt.Fprintf(os.Stderr, "  - Removes optional properties with union types\n")
		fmt.Fprintf(os.Stderr, "  - Reduces required union properties to single variant (first variant)\n")
		fmt.Fprintf(os.Stderr, "  - Removes all extension fields (x-*) and examples\n")
		fmt.Fprintf(os.Stderr, "  - Optionally limits number of optional properties per schema\n")
		fmt.Fprintf(os.Stderr, "  - With -config: applies oapi-codegen-dd filter+overlay+prune\n")
		fmt.Fprintf(os.Stderr, "    BEFORE simplification (filter paths/tags/operation-ids,\n")
		fmt.Fprintf(os.Stderr, "    apply OpenAPI Overlay 1.0 deltas, drop dangling refs)\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Simplify unions and keep 5 optional properties per schema (default)\n")
		fmt.Fprintf(os.Stderr, "  go run %s openapi.yml\n\n", cmdPath)
		fmt.Fprintf(os.Stderr, "  # Apply codegen.yml filter+overlay before simplification\n")
		fmt.Fprintf(os.Stderr, "  go run %s -config codegen.yml -output simplified.yml openapi.yml\n\n", cmdPath)
		fmt.Fprintf(os.Stderr, "  # Keep exactly 3 optional properties per schema\n")
		fmt.Fprintf(os.Stderr, "  go run %s -keep-optional 3 openapi.yml\n\n", cmdPath)
		fmt.Fprintf(os.Stderr, "  # Keep 1-3 optional properties (alphabetically first) per schema\n")
		fmt.Fprintf(os.Stderr, "  go run %s -min-optional-properties 1 -max-optional-properties 3 openapi.yml\n", cmdPath)
	}
}

func main() {
	flag.BoolVar(&flagPrintUsage, "help", false, "Show this help and exit.")
	flag.StringVar(&flagConfig, "config", "", "Path to oapi-codegen-dd codegen.yml. When set, the spec is run through filter+overlay+prune (using cfg.Filter and cfg.Overlay) before simplification.")
	flag.StringVar(&flagOutput, "output", "", "Output file path. If not specified, outputs to stdout.")
	flag.IntVar(&flagKeepOptional, "keep-optional", 5, "Keep exactly this many optional properties per schema. (default 5)")
	flag.IntVar(&flagMinOptionalProperties, "min-optional-properties", 0, "Minimum number of optional properties to keep (overrides -keep-optional, used with -max-optional-properties).")
	flag.IntVar(&flagMaxOptionalProperties, "max-optional-properties", 0, "Maximum number of optional properties to keep (overrides -keep-optional, used with -min-optional-properties).")

	flag.Parse()

	if flagPrintUsage {
		flag.Usage()
		os.Exit(0)
	}

	// Require exactly one argument (spec path)
	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "Error: Expected exactly one argument (path to OpenAPI spec).\n\n")
		fmt.Fprintf(os.Stderr, "Run with -help for more information.\n")
		os.Exit(1)
	}

	specPath := flag.Arg(0)

	// Read the spec file
	specContents, err := files.ReadFileOrURL(specPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading spec file: %v\n", err)
		os.Exit(1)
	}

	doc, err := loadDocument(specContents)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading OpenAPI spec: %v\n", err)
		os.Exit(1)
	}

	// Create optional property config based on flags
	var optConfig *typedef.OptionalPropertyConfig

	if flagMinOptionalProperties > 0 || flagMaxOptionalProperties > 0 {
		// Range mode takes precedence
		optConfig = &typedef.OptionalPropertyConfig{
			Min: flagMinOptionalProperties,
			Max: flagMaxOptionalProperties,
		}
	} else if flagKeepOptional > 0 {
		// Fixed number mode (default is 5)
		optConfig = &typedef.OptionalPropertyConfig{
			Min: flagKeepOptional,
			Max: flagKeepOptional,
		}
	}
	// If optConfig is nil, keep all optional properties

	// Build and simplify the model
	model, err := typedef.BuildModel(doc, true, optConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error simplifying document: %v\n", err)
		os.Exit(1)
	}

	// Render the simplified model
	rendered, err := model.Render()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error rendering simplified document: %v\n", err)
		os.Exit(1)
	}

	// Output to file or stdout
	if flagOutput != "" {
		if err := os.WriteFile(flagOutput, rendered, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output file: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Simplified spec written to: %s\n", flagOutput)
	} else {
		fmt.Print(string(rendered))
	}
}

// loadDocument parses the OpenAPI bytes and, when -config is supplied,
// runs them through oapi-codegen-dd's filter + overlay + prune pipeline
// before simplification. Without -config it falls back to the historical
// behaviour: a plain libopenapi document with circular-ref check disabled.
func loadDocument(specContents []byte) (libopenapi.Document, error) {
	if flagConfig == "" {
		docConfig := &datamodel.DocumentConfiguration{
			SkipCircularReferenceCheck: true,
		}
		return libopenapi.NewDocumentWithConfiguration(specContents, docConfig)
	}

	cfgBytes, err := os.ReadFile(flagConfig)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", flagConfig, err)
	}

	var cfg codegen.Configuration
	if err := yaml.Unmarshal(cfgBytes, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", flagConfig, err)
	}
	cfg = cfg.WithDefaults()

	// CreateDocument applies overlay (cfg.Overlay) -> filter (cfg.Filter)
	// -> prune (drops dangling refs). Same pipeline used by `mockzilla
	// gen-service`, so users can share one codegen.yml between codegen and
	// simplify and get the same surface in both.
	return codegen.CreateDocument(specContents, cfg)
}
