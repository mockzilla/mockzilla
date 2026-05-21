package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/mockzilla/mockzilla/v2/internal/files"
	"github.com/mockzilla/mockzilla/v2/internal/simplify"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/spf13/cobra"
)

func simplifyCommand() *cobra.Command {
	var (
		flagConfig      string
		flagOutput      string
		flagOptional    int
		flagOptionalMin int
		flagOptionalMax int
	)

	cmd := &cobra.Command{
		Use:   "simplify [flags] <spec>",
		Short: "Simplify an OpenAPI spec by removing unions and limiting optional properties",
		Long: `Simplify an OpenAPI spec by removing or reducing union types (anyOf/oneOf).

The command:
  - Removes optional properties with union types
  - Reduces required union properties to a single variant (first variant)
  - Strips x-* extension fields from schemas
  - Optionally limits the number of optional properties per schema
  - With --config: applies oapi-codegen-dd filter + overlay + prune BEFORE
    simplification (filter paths/tags/operation-ids, apply OpenAPI Overlay 1.0
    deltas, drop dangling refs)

Examples are intentionally preserved to avoid dangling $ref targets.

Optional-property handling:
  (none)               keep every optional property
  --optional N         keep exactly N optional properties per schema
  --optional 0         drop every optional property
  --optional-min A
  --optional-max B     range mode: keep a random number between A and B
                       (both flags must be supplied together)

Use '-' as <spec> to read the spec from stdin.

Examples:
  mockzilla simplify openapi.yml
  mockzilla simplify --output simplified.yml --optional 5 openapi.yml
  mockzilla simplify --config codegen.yml -o simplified.yml openapi.yml
  curl -s https://example.com/openapi.json | mockzilla simplify -`,
		Args:          requireSpecArg,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			specBytes, err := readSpec(args[0])
			if err != nil {
				return fmt.Errorf("reading spec: %w", err)
			}

			var configBytes []byte
			if flagConfig != "" {
				configBytes, err = os.ReadFile(flagConfig)
				if err != nil {
					return fmt.Errorf("reading config %q: %w", flagConfig, err)
				}
			}

			output, err := simplify.Simplify(specBytes, simplify.Options{
				ConfigYAML:         configBytes,
				OptionalProperties: buildOptionalConfig(cmd, flagOptional, flagOptionalMin, flagOptionalMax),
			})
			if err != nil {
				return err
			}

			return writeOutput(output, flagOutput)
		},
	}

	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path (default stdout; '-' also means stdout)")
	cmd.Flags().StringVarP(&flagConfig, "config", "c", "", "Path to oapi-codegen-dd codegen.yml. Applies filter + overlay + prune before simplification.")
	cmd.Flags().IntVar(&flagOptional, "optional", 0, "Keep exactly N optional properties per schema (unset = keep all; 0 = drop all)")
	cmd.Flags().IntVar(&flagOptionalMin, "optional-min", 0, "Minimum optional properties (use with --optional-max)")
	cmd.Flags().IntVar(&flagOptionalMax, "optional-max", 0, "Maximum optional properties (use with --optional-min)")

	cmd.MarkFlagsMutuallyExclusive("optional", "optional-min")
	cmd.MarkFlagsMutuallyExclusive("optional", "optional-max")
	cmd.MarkFlagsRequiredTogether("optional-min", "optional-max")

	return cmd
}

// requireSpecArg replaces cobra.ExactArgs(1) so that calling `mockzilla simplify`
// with no arguments produces a friendly, actionable message instead of the stock
// "accepts 1 arg(s), received 0".
func requireSpecArg(_ *cobra.Command, args []string) error {
	switch len(args) {
	case 1:
		return nil
	case 0:
		return errors.New(`missing <spec> argument

Examples:
  mockzilla simplify openapi.yml
  mockzilla simplify https://example.com/openapi.json
  cat openapi.yml | mockzilla simplify -

Run 'mockzilla simplify --help' for all options`)
	default:
		return fmt.Errorf("expected one <spec> argument, got %d: %v", len(args), args)
	}
}

// buildOptionalConfig translates the three CLI flags into the config struct.
// Returns nil when the user supplied no optional-property flags (= keep all).
func buildOptionalConfig(cmd *cobra.Command, fixed, min, max int) *config.OptionalProperties {
	switch {
	case cmd.Flags().Changed("optional-min"), cmd.Flags().Changed("optional-max"):
		return &config.OptionalProperties{Min: min, Max: max}
	case cmd.Flags().Changed("optional"):
		return &config.OptionalProperties{Min: fixed, Max: fixed}
	default:
		return nil
	}
}

func readSpec(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return files.ReadFileOrURL(path)
}

func writeOutput(content []byte, out string) error {
	if out == "" || out == "-" {
		_, err := os.Stdout.Write(content)
		return err
	}
	if err := os.WriteFile(out, content, 0644); err != nil {
		return fmt.Errorf("writing output file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Simplified spec written to: %s\n", out)
	return nil
}
