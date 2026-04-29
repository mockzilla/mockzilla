package api

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/cmd/gen/templatehelpers"
	"github.com/mockzilla/mockzilla/v2/internal/files"
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/typedef"
	"go.yaml.in/yaml/v4"
)

//go:embed templates/*
var templatesFS embed.FS

// ServiceOptions contains options for generating service files.
type ServiceOptions struct {
	Name string // Optional: service name (default: inferred from directory name)

	// Optional: output directory for the service (default: current directory's parent)
	// When provided, setup dir will be <OutputDir>/setup/
	OutputDir string

	// Optional: path or URL to OpenAPI spec (default: openapi.yml/json in setup dir)
	SpecPath string

	// Optional: "openapi" or "static" (inferred from source if not provided)
	ServiceType string

	// Optional: maximum recursion depth for circular schemas
	MaxRecursionDepth int

	// Optional: maximum number of endpoints to process (0 = no limit, for debugging)
	MaxEndpoints int

	// Optional: path to custom codegen.yml to merge with template
	CodegenConfigPath string

	// Optional: path to custom config.yml to merge with template
	ServiceConfigPath string

	// Optional: suppress output messages (useful for automated tests)
	Quiet bool
}

// setupTemplateData contains data for rendering setup templates.
type setupTemplateData struct {
	ServiceName string // User-provided name (e.g., "pet/store/v1")
	PackageName string // Valid Go package name derived from ServiceName (e.g., "pet_store_v1")
	SpecPath    string // Path or URL to OpenAPI spec (empty for local openapi.yml/json)
	ServiceType string // "openapi" or "static"
}

// GenerateService generates all service files (types, handlers, register.go, middleware.go).
// If OutputDir is provided, creates the service directory structure including setup/.
// If run from an existing setup/ directory, uses that directory for configuration.
// The function:
//   - Ensures setup directory exists with required files (creates if missing)
//   - Generates types in <service>/types/
//   - Generates handlers in <service>/handler/
//   - Generates register.go in <service>/
//   - Generates middleware.go in <service>/ (only if it doesn't exist)
//   - Infers the service name from the directory name
func GenerateService(opts ServiceOptions) error {
	var setupDir string
	var serviceDir string

	// Default output directory:
	// - From a service directory (has setup/): use current directory (go generate)
	// - From project root: use services directory from config
	if opts.OutputDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting current directory: %w", err)
		}
		if fileExists(filepath.Join(cwd, "setup")) {
			opts.OutputDir = cwd
		} else {
			opts.OutputDir = config.NewPaths(cwd).Services
		}
	}

	// If name is provided and output directory doesn't end with the name,
	// append the name to create the service subdirectory.
	// Use HasSuffix with cleaned paths to handle multi-segment names (e.g., "hello/world/v2").
	if opts.Name != "" && !strings.HasSuffix(filepath.Clean(opts.OutputDir), filepath.Clean(opts.Name)) {
		opts.OutputDir = filepath.Join(opts.OutputDir, opts.Name)
	}

	// Setup will be at <OutputDir>/setup/
	serviceDir = opts.OutputDir
	setupDir = filepath.Join(opts.OutputDir, "setup")

	// Infer service name from directory name if not provided
	if opts.Name == "" {
		opts.Name = filepath.Base(serviceDir)
	}

	// Ensure setup directory exists with all required files
	if err := ensureSetupDir(opts, serviceDir, setupDir); err != nil {
		return fmt.Errorf("ensuring setup directory: %w", err)
	}

	// File paths relative to setup directory
	configFile := filepath.Join(setupDir, "codegen.yml")
	serviceConfigFile := filepath.Join(setupDir, "config.yml")

	// For static services, regenerate openapi.yml from the data directory
	// This ensures changes to static files are reflected in the spec
	staticDataDir := filepath.Join(setupDir, "data")
	if dirHasFiles(staticDataDir) && isStaticService(staticDataDir, opts.ServiceType) {
		specContents, err := GenerateSpecFromStaticDir(staticDataDir, opts.Name)
		if err != nil {
			return fmt.Errorf("generating spec from static directory %s: %w", staticDataDir, err)
		}

		specPath := filepath.Join(setupDir, "openapi.yml")
		if err := os.WriteFile(specPath, specContents, 0644); err != nil {
			return fmt.Errorf("writing generated spec to %s: %w", specPath, err)
		}
		if !opts.Quiet {
			fmt.Printf("Regenerated OpenAPI spec from static files: %s\n", specPath)
		}
	}

	// Find OpenAPI spec file
	// For static services, the spec was just regenerated above
	// For OpenAPI services with a URL or file path, use that directly
	var specFile string
	if opts.SpecPath != "" && !isStaticService(opts.SpecPath, opts.ServiceType) {
		// Use provided spec path/URL (only for non-static services)
		specFile = opts.SpecPath
	} else {
		// Fall back to local files (try both .yml and .json extensions)
		// This is also used for static services where the spec was generated
		specFile = filepath.Join(setupDir, "openapi.yml")
		if _, err := os.Stat(specFile); os.IsNotExist(err) {
			specFile = filepath.Join(setupDir, "openapi.json")
			if _, err := os.Stat(specFile); os.IsNotExist(err) {
				return fmt.Errorf("openapi spec file not found in %s (tried openapi.yml and openapi.json)", setupDir)
			}
		}
	}

	// Verify required files exist
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return fmt.Errorf("codegen.yml not found in %s", setupDir)
	}
	if _, err := os.Stat(serviceConfigFile); os.IsNotExist(err) {
		return fmt.Errorf("config.yml not found in %s", setupDir)
	}

	// Read the spec file
	specContents, err := files.ReadFileOrURL(specFile)
	if err != nil {
		return fmt.Errorf("reading spec file: %w", err)
	}

	// If spec was fetched from URL, save it locally for //go:embed to work
	if files.IsURL(specFile) {
		specExt := ".yml"
		if files.IsJsonType(specContents) {
			specExt = ".json"
		}
		localSpecPath := filepath.Join(setupDir, "openapi"+specExt)
		if err := os.WriteFile(localSpecPath, specContents, 0644); err != nil {
			return fmt.Errorf("saving spec file to %s: %w", localSpecPath, err)
		}
		if !opts.Quiet {
			fmt.Printf("Saved spec to: %s\n", localSpecPath)
		}
	}

	// Read the codegen config file
	cfg := codegen.Configuration{}
	codegenCfgContents, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}
	if err = yaml.Unmarshal(codegenCfgContents, &cfg); err != nil {
		return fmt.Errorf("parsing config file: %w", err)
	}
	cfg = cfg.WithDefaults()

	// Resolve overlay paths to be absolute (relative to setup directory)
	if cfg.Overlay != nil && len(cfg.Overlay.Sources) > 0 {
		// Save original filenames for go:embed directives in generated code
		if cfg.UserContext == nil {
			cfg.UserContext = make(map[string]any)
		}
		overlayFiles := make([]string, len(cfg.Overlay.Sources))
		copy(overlayFiles, cfg.Overlay.Sources)
		cfg.UserContext["OverlayFiles"] = overlayFiles

		for i, src := range cfg.Overlay.Sources {
			if !filepath.IsAbs(src) && !files.IsURL(src) {
				cfg.Overlay.Sources[i] = filepath.Join(setupDir, src)
			}
		}
	}

	// Read the service config file
	serviceCfgContents, err := os.ReadFile(serviceConfigFile)
	if err != nil {
		return fmt.Errorf("reading service config file: %w", err)
	}
	serviceCfg, err := config.NewServiceConfigFromBytes(serviceCfgContents)
	if err != nil {
		return fmt.Errorf("parsing service config: %w", err)
	}

	// Parse OpenAPI spec
	parseCtx, errs := typedef.CreateParseContext(specContents, cfg, serviceCfg.SpecOptions)
	if len(errs) > 0 {
		return fmt.Errorf("parsing OpenAPI spec: %v", errs[0])
	}
	if len(parseCtx.Operations) == 0 {
		slog.Warn("No operations found in spec")
	}

	// Determine output directories
	// The output directory in codegen.yml is relative to setup directory
	destDir := ""
	if cfg.Output != nil {
		// Make the output directory relative to setup directory, not current working directory
		destDir = filepath.Join(setupDir, cfg.Output.Directory)
		if err = os.MkdirAll(destDir, generatedDirPerm); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}
	}

	if destDir == "" {
		return fmt.Errorf("no output directory specified")
	}

	// Override oapi-codegen templates with mockzilla versions
	if cfg.UserTemplates == nil {
		cfg.UserTemplates = make(map[string]string)
	}

	// Resolve user template file paths relative to setup directory.
	// Single-line values are treated as file paths by oapi-codegen.
	for key, val := range cfg.UserTemplates {
		if !strings.Contains(val, "\n") && !filepath.IsAbs(val) {
			cfg.UserTemplates[key] = filepath.Join(setupDir, val)
		}
	}

	// Override handler templates (scaffold templates + service-options)
	// Skip templates already provided by the user in codegen.yml user-templates.
	for _, tmplName := range []string{"service.tmpl", "server.tmpl", "middleware.tmpl", "service-options.tmpl"} {
		key := "handler/" + tmplName
		if _, ok := cfg.UserTemplates[key]; ok {
			continue
		}
		tmplContent, err := templatesFS.ReadFile("templates/handler/" + tmplName)
		if err != nil {
			return fmt.Errorf("reading %s template: %w", tmplName, err)
		}
		cfg.UserTemplates[key] = string(tmplContent)
	}

	// Override chi handler template to include mockzilla generator code
	if _, ok := cfg.UserTemplates["handler/chi/handler.tmpl"]; !ok {
		chiHandlerContent, err := templatesFS.ReadFile("templates/handler/chi/handler.tmpl")
		if err != nil {
			return fmt.Errorf("reading chi/handler.tmpl: %w", err)
		}
		cfg.UserTemplates["handler/chi/handler.tmpl"] = string(chiHandlerContent)
	}

	// Add imports required by mockzilla generator code
	cfg.AdditionalImports = append(cfg.AdditionalImports,
		codegen.AdditionalImport{Package: "sync"},
		codegen.AdditionalImport{Package: "github.com/mockzilla/mockzilla/v2/pkg/api"},
		codegen.AdditionalImport{Package: "github.com/mockzilla/mockzilla/v2/pkg/config"},
		codegen.AdditionalImport{Package: "github.com/mockzilla/mockzilla/v2/pkg/db"},
		codegen.AdditionalImport{Package: "github.com/mockzilla/mockzilla/v2/pkg/factory"},
		codegen.AdditionalImport{Package: "github.com/mockzilla/mockzilla/v2/pkg/schema"},
		codegen.AdditionalImport{Package: "github.com/mockzilla/mockzilla/v2/pkg/generator"},
		codegen.AdditionalImport{Package: "github.com/mockzilla/mockzilla/v2/pkg/loader"},
		codegen.AdditionalImport{Package: "github.com/mockzilla/mockzilla/v2/pkg/typedef"},
		codegen.AdditionalImport{Alias: "oapicodegen", Package: "github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"},
		codegen.AdditionalImport{Alias: "yamlv4", Package: "go.yaml.in/yaml/v4"},
	)

	// Add libopenapi import for runtime overlay application
	if cfg.Overlay != nil && len(cfg.Overlay.Sources) > 0 {
		cfg.AdditionalImports = append(cfg.AdditionalImports,
			codegen.AdditionalImport{Package: "github.com/pb33f/libopenapi"},
		)
	}

	// Step 1: Generate code with oapi-codegen
	generatedCode, err := codegen.Generate(specContents, cfg)
	if err != nil {
		return fmt.Errorf("oapi-codegen generate: %w", err)
	}

	// Determine handler directory for mockzilla templates
	handlerDir := destDir
	if cfg.Generate != nil && cfg.Generate.Handler != nil {
		if cfg.Generate.Handler.Output != nil && cfg.Generate.Handler.Output.Directory != "" {
			handlerDir = filepath.Join(destDir, cfg.Generate.Handler.Output.Directory)
			if err := os.MkdirAll(handlerDir, generatedDirPerm); err != nil {
				return fmt.Errorf("creating handler directory %s: %w", handlerDir, err)
			}
		}
	}

	// Write combined file to handler directory
	// With use-single-file: true and skip-fmt: true, GetCombined() returns unformatted code
	// Generator code is now included via the chi/handler.tmpl override
	outputFilename := "gen.go"
	if cfg.Output != nil && cfg.Output.Filename != "" {
		outputFilename = cfg.Output.Filename
	}
	outputPath := filepath.Join(handlerDir, outputFilename)
	formatted, err := codegen.FormatCode(generatedCode.GetCombined())
	if err != nil {
		return fmt.Errorf("formatting combined code: %w", err)
	}
	if err := os.WriteFile(outputPath, []byte(formatted), generatedFilePerm); err != nil {
		return fmt.Errorf("writing %s: %w", outputFilename, err)
	}
	if !opts.Quiet {
		fmt.Printf("Generated: %s\n", outputPath)
	}

	// Write scaffold files (service.go, middleware.go) - these are user-editable
	for key, content := range generatedCode {
		if !codegen.IsScaffoldFile(key) {
			continue
		}

		scaffoldPath := codegen.ScaffoldFileName(key) + ".go"
		actualFilename := filepath.Base(scaffoldPath)
		scaffoldDir := filepath.Dir(scaffoldPath)
		outputDir := destDir
		if scaffoldDir != "." {
			outputDir = filepath.Join(destDir, scaffoldDir)
		}

		if err := os.MkdirAll(outputDir, generatedDirPerm); err != nil {
			return fmt.Errorf("creating scaffold directory %s: %w", outputDir, err)
		}

		filePath := filepath.Join(outputDir, actualFilename)

		// Check overwrite setting
		scaffoldOutput := cfg.Generate.Handler.ResolveScaffoldOutput(cfg.Output)
		if !scaffoldOutput.Overwrite {
			if _, err := os.Stat(filePath); err == nil {
				slog.Info("Skipping scaffold file (already exists)", "file", filePath)
				continue
			}
		}

		formattedCode, err := codegen.FormatCode(content)
		if err != nil {
			return fmt.Errorf("formatting %s: %w", actualFilename, err)
		}

		if err := os.WriteFile(filePath, []byte(formattedCode), generatedFilePerm); err != nil {
			return fmt.Errorf("writing %s: %w", actualFilename, err)
		}
		if !opts.Quiet {
			fmt.Printf("Generated: %s\n", filePath)
		}
	}

	slog.Info("Service generation complete")
	return nil
}

// ensureSetupDir creates the setup directory with all necessary files if it doesn't exist.
// generate.go is placed in serviceDir (service root), other config files go in setupDir.
func ensureSetupDir(opts ServiceOptions, serviceDir, setupDir string) error {
	// Check if setup dir already exists with required files
	configFile := filepath.Join(setupDir, "codegen.yml")
	serviceConfigFile := filepath.Join(setupDir, "config.yml")
	generateFile := filepath.Join(serviceDir, "generate.go") // generate.go goes in service root

	// Check if all required files exist
	configExists := fileExists(configFile)
	serviceConfigExists := fileExists(serviceConfigFile)
	generateExists := fileExists(generateFile)

	// If all required files exist, setup is complete
	if configExists && serviceConfigExists && generateExists {
		return nil
	}

	// Use the user-provided name as-is for the service name.
	// NormalizeServiceName uses filepath.Base which would strip path segments
	// (e.g., "a/b/c/v1" would become just "v1").
	serviceName := opts.Name
	if serviceName == "" {
		return fmt.Errorf("service name is required for initial setup")
	}

	// Determine service type
	serviceType := opts.ServiceType
	if opts.SpecPath != "" {
		// Check if source is a URL
		isURL := files.IsURL(opts.SpecPath)

		if isURL {
			// URLs are always OpenAPI specs
			inferredType := "openapi"
			if serviceType != "" && serviceType != inferredType {
				return fmt.Errorf("type mismatch: URLs must be OpenAPI specs, but type was specified as %s", serviceType)
			}
			serviceType = inferredType
		} else {
			// Infer type from local file/directory
			info, err := os.Stat(opts.SpecPath)
			if err != nil {
				return fmt.Errorf("accessing source path %s: %w", opts.SpecPath, err)
			}
			inferredType := "openapi"
			if info.IsDir() {
				inferredType = "static"
			}

			// Validate against provided type if specified
			if serviceType != "" && serviceType != inferredType {
				return fmt.Errorf("type mismatch: specified %s but source appears to be %s", serviceType, inferredType)
			}
			serviceType = inferredType
		}
	} else {
		// No source - default to openapi
		if serviceType == "" {
			serviceType = "openapi"
		}
		if serviceType != "openapi" && serviceType != "static" {
			return fmt.Errorf("service type must be 'openapi' or 'static', got: %s", serviceType)
		}
	}

	// Get template files
	tplFiles, err := getSetupTemplateFiles(serviceType)
	if err != nil {
		return fmt.Errorf("getting templates: %w", err)
	}

	// Create directory structure
	if err := os.MkdirAll(setupDir, 0755); err != nil {
		return fmt.Errorf("creating setup directory: %w", err)
	}

	// For static services, populate the data directory with sample files
	if serviceType == "static" {
		if err := copyEmbeddedDir(templatesFS, "templates/setup/data", filepath.Join(setupDir, "data")); err != nil {
			return fmt.Errorf("creating setup data directory: %w", err)
		}
	}

	// Prepare template data
	// For URLs, we keep the URL as SpecPath so it's embedded in generate.go
	// For local OpenAPI files, we leave SpecPath empty (will fallback to local openapi.yml/json)
	specPath := ""
	if files.IsURL(opts.SpecPath) {
		specPath = opts.SpecPath
	}
	data := setupTemplateData{
		ServiceName: serviceName,
		PackageName: types.ToSnakeCase(serviceName),
		SpecPath:    specPath,
		ServiceType: serviceType,
	}

	// Render and write template files
	for filename, templatePath := range tplFiles {
		// Skip openapi.yml template if we have a source file (we'll copy it later)
		if filename == "openapi.yml" && opts.SpecPath != "" {
			continue
		}

		// Render all files as templates (files without directives pass through unchanged)
		rendered, err := renderSetupTemplate(templatePath, data)
		if err != nil {
			return fmt.Errorf("rendering template %s: %w", templatePath, err)
		}
		content := []byte(rendered)

		// generate.go goes in service root, other files go in setup dir
		var path string
		if filename == "generate.go" {
			path = filepath.Join(serviceDir, filename)
		} else {
			path = filepath.Join(setupDir, filename)
		}

		// Ensure directory exists before writing
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", filename, err)
		}
		if err := os.WriteFile(path, content, 0644); err != nil {
			return fmt.Errorf("writing %s: %w", filename, err)
		}
		if !opts.Quiet {
			fmt.Printf("Generated: %s\n", path)
		}
	}

	// Merge custom codegen config if provided
	if opts.CodegenConfigPath != "" {
		currentPath := filepath.Join(setupDir, "codegen.yml")
		err := mergeSetupYAMLConfigs(opts.CodegenConfigPath, currentPath, "codegen", func(templateData, customData []byte) ([]byte, error) {
			var templateCfg, customCfg codegen.Configuration
			if err := yaml.Unmarshal(templateData, &templateCfg); err != nil {
				return nil, fmt.Errorf("parsing template codegen config: %w", err)
			}
			if err := yaml.Unmarshal(customData, &customCfg); err != nil {
				return nil, fmt.Errorf("parsing custom codegen config: %w", err)
			}

			mergedCfg := templateCfg.WithDefaults().OverwriteWith(customCfg)
			return yaml.Dump(mergedCfg, yaml.WithIndent(2))
		})
		if err != nil {
			return err
		}
	}

	// Merge custom service config if provided
	if opts.ServiceConfigPath != "" {
		currentPath := filepath.Join(setupDir, "config.yml")
		err := mergeSetupYAMLConfigs(opts.ServiceConfigPath, currentPath, "service", func(templateData, customData []byte) ([]byte, error) {
			templateCfg, err := config.NewServiceConfigFromBytes(templateData)
			if err != nil {
				return nil, fmt.Errorf("parsing template service config: %w", err)
			}
			customCfg, err := config.NewServiceConfigFromBytes(customData)
			if err != nil {
				return nil, fmt.Errorf("parsing custom service config: %w", err)
			}

			mergedCfg := templateCfg.OverwriteWith(customCfg)
			return yaml.Dump(mergedCfg, yaml.WithIndent(2))
		})
		if err != nil {
			return err
		}
	}

	// Handle source based on type
	if err := handleSetupSourceSpec(opts, setupDir, serviceName, serviceType); err != nil {
		return err
	}

	if !opts.Quiet {
		fmt.Printf("\n✅ Setup directory created at %s\n", setupDir)
	}

	return nil
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// dirHasFiles checks if a directory exists and contains at least one file.
func dirHasFiles(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// copyEmbeddedDir copies a directory tree from the embedded FS to the local filesystem.
func copyEmbeddedDir(fsys embed.FS, srcDir, destDir string) error {
	return fs.WalkDir(fsys, srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(destDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		content, err := fsys.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, content, 0644)
	})
}

// isStaticService checks if the service is a static service based on the spec path and service type.
func isStaticService(specPath, serviceType string) bool {
	if serviceType == "static" {
		return true
	}
	// If no explicit type, check if the path is a directory
	if specPath != "" && !files.IsURL(specPath) {
		info, err := os.Stat(specPath)
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// handleSetupSourceSpec handles the source spec based on type (static, URL, or local file).
func handleSetupSourceSpec(opts ServiceOptions, setupDir, serviceName, serviceType string) error {
	if opts.SpecPath == "" {
		return nil
	}

	if serviceType == "static" {
		// Copy static files into setup/data/ so they're self-contained
		dataDir := filepath.Join(setupDir, "data")
		if err := files.CopyDirectory(opts.SpecPath, dataDir); err != nil {
			return fmt.Errorf("copying static files from %s to %s: %w", opts.SpecPath, dataDir, err)
		}
		if !opts.Quiet {
			fmt.Printf("Copied static files to: %s\n", dataDir)
		}

		specContents, err := GenerateSpecFromStaticDir(dataDir, serviceName)
		if err != nil {
			return fmt.Errorf("generating spec from static directory %s: %w", dataDir, err)
		}
		specPath := filepath.Join(setupDir, "openapi.yml")
		if err := os.WriteFile(specPath, specContents, 0644); err != nil {
			return fmt.Errorf("writing generated spec to %s: %w", specPath, err)
		}
		if !opts.Quiet {
			fmt.Printf("Generated OpenAPI spec from static files: %s\n", specPath)
		}
		return nil
	}

	if files.IsURL(opts.SpecPath) {
		if !opts.Quiet {
			fmt.Printf("Using remote spec URL: %s\n", opts.SpecPath)
		}
		return nil
	}

	// Local file - copy it
	specContents, err := files.ReadFileOrURL(opts.SpecPath)
	if err != nil {
		return fmt.Errorf("reading spec file from %s: %w", opts.SpecPath, err)
	}

	specExt := ".yml"
	if files.IsJsonType(specContents) {
		specExt = ".json"
	}

	specPath := filepath.Join(setupDir, "openapi"+specExt)
	if err := os.MkdirAll(filepath.Dir(specPath), 0755); err != nil {
		return fmt.Errorf("creating directory for spec file: %w", err)
	}

	if err := os.WriteFile(specPath, specContents, 0644); err != nil {
		return fmt.Errorf("writing spec file to %s: %w", specPath, err)
	}

	if !opts.Quiet {
		fmt.Printf("Copied spec file to: %s\n", specPath)
	}
	return nil
}

// getSetupTemplateFiles returns the appropriate template files based on service type.
func getSetupTemplateFiles(serviceType string) (map[string]string, error) {
	// Determine which openapi template to use
	openapiTemplate := "templates/setup/openapi.yml"
	if serviceType == "static" {
		openapiTemplate = "templates/setup/openapi.static.yml"
	}

	templates := map[string]string{
		"generate.go": "templates/setup/generate.go.tmpl",
		"codegen.yml": "templates/setup/codegen.yml",
		"config.yml":  "templates/setup/config.yml",
		"context.yml": "templates/setup/context.yml",
		"openapi.yml": openapiTemplate,
	}

	return templates, nil
}

// mergeSetupYAMLConfigs reads custom and template YAML files, unmarshals them,
// merges using the provided merge function, and writes back the result
func mergeSetupYAMLConfigs(customPath, templatePath, configType string, mergeFn func(templateData, customData []byte) ([]byte, error)) error {
	customContent, err := os.ReadFile(customPath)
	if err != nil {
		return fmt.Errorf("reading custom %s config from %s: %w", configType, customPath, err)
	}

	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("reading template %s config: %w", configType, err)
	}

	mergedContent, err := mergeFn(templateContent, customContent)
	if err != nil {
		return err
	}

	if err := os.WriteFile(templatePath, mergedContent, 0644); err != nil {
		return fmt.Errorf("writing merged %s config to %s: %w", configType, templatePath, err)
	}

	return nil
}

// renderSetupTemplate renders a setup template file with the given data.
func renderSetupTemplate(templatePath string, data setupTemplateData) (string, error) {
	content, err := templatesFS.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("reading template file: %w", err)
	}

	tmpl, err := template.New(templatePath).Funcs(templatehelpers.GetFuncMap()).Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}
