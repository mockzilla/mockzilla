package main

import (
	"flag"
	"fmt"
	"os"

	cmdapi "github.com/mockzilla/mockzilla/v2/cmd/api"
)

var (
	flagPrintUsage        bool
	flagName              string
	flagOutput            string
	flagType              string
	flagMaxRecursionDepth int
	flagMaxEndpoints      int
	flagCodegenConfig     string
	flagServiceConfig     string
	flagQuiet             bool
)

const cmdPath = "github.com/mockzilla/mockzilla/v2/cmd/gen/service"

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: go run %s [options] [spec]\n\n", cmdPath)
		fmt.Fprintf(os.Stderr, "Generates a complete service from OpenAPI spec or static files.\n")
		fmt.Fprintf(os.Stderr, "Creates setup directory if it doesn't exist, then generates types, handlers, register.go, and middleware.go.\n\n")
		fmt.Fprintf(os.Stderr, "Arguments:\n")
		fmt.Fprintf(os.Stderr, "  spec    Path or URL to OpenAPI spec, or path to static files directory\n")
		fmt.Fprintf(os.Stderr, "          (default: openapi.yml/json in setup directory)\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # First run: create service from spec\n")
		fmt.Fprintf(os.Stderr, "  go run %s -name petstore https://petstore3.swagger.io/api/v3/openapi.json\n\n", cmdPath)
		fmt.Fprintf(os.Stderr, "  # First run with custom output directory\n")
		fmt.Fprintf(os.Stderr, "  go run %s -name myapi -output /path/to/service openapi.yml\n\n", cmdPath)
		fmt.Fprintf(os.Stderr, "  # Regenerate from existing setup (run from setup directory)\n")
		fmt.Fprintf(os.Stderr, "  go run %s\n\n", cmdPath)
		fmt.Fprintf(os.Stderr, "  # Create service from static files\n")
		fmt.Fprintf(os.Stderr, "  go run %s -name myapi -type static /path/to/static\n", cmdPath)
	}
}

func main() {
	flag.BoolVar(&flagPrintUsage, "help", false, "Show this help and exit.")
	flag.StringVar(&flagName, "name", "", "Service name (default: inferred from directory or spec name)")
	flag.StringVar(&flagOutput, "output", "", "Output directory for service (default: resources/data/services/<name>)")
	flag.StringVar(&flagType, "type", "", "Service type: 'openapi' or 'static' (default: inferred from source)")
	flag.IntVar(&flagMaxRecursionDepth, "max-recursion-depth", 0, "Maximum recursion depth for circular schemas (0 = unlimited)")
	flag.IntVar(&flagMaxEndpoints, "max-endpoints", 0, "Maximum number of endpoints to process (0 = all, for debugging)")
	flag.StringVar(&flagCodegenConfig, "codegen-config", "", "Path to custom codegen.yml to merge with template")
	flag.StringVar(&flagServiceConfig, "service-config", "", "Path to custom config.yml to merge with template")
	flag.BoolVar(&flagQuiet, "quiet", false, "Suppress non-error output")

	flag.Parse()

	if flagPrintUsage {
		flag.Usage()
		os.Exit(0)
	}

	// Optional positional argument for spec path/URL
	if flag.NArg() > 1 {
		fmt.Fprintf(os.Stderr, "Error: Too many arguments.\n\n")
		fmt.Fprintf(os.Stderr, "Run with -help for usage information.\n")
		os.Exit(1)
	}

	specPath := flag.Arg(0) // empty string if not provided

	err := cmdapi.GenerateService(cmdapi.ServiceOptions{
		Name:              flagName,
		OutputDir:         flagOutput,
		SpecPath:          specPath,
		ServiceType:       flagType,
		MaxRecursionDepth: flagMaxRecursionDepth,
		MaxEndpoints:      flagMaxEndpoints,
		CodegenConfigPath: flagCodegenConfig,
		ServiceConfigPath: flagServiceConfig,
		Quiet:             flagQuiet,
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
