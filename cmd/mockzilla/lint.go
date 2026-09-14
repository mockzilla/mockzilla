package main

import (
	"encoding/json"
	"fmt"

	"github.com/mockzilla/mockzilla/v2/pkg/lint"
	"github.com/spf13/cobra"
)

func lintCommand() *cobra.Command {
	var flagFormat string

	cmd := &cobra.Command{
		Use:   "lint [flags] <spec>",
		Short: "Find schemas in an OpenAPI spec that no value can satisfy",
		Long: `Lint an OpenAPI spec for schemas that no value can satisfy, such as
an array with a scalar enum, or additionalProperties: false next to oneOf
properties. Specs in the wild ship with these defects. Mockzilla cannot
generate a valid response for them, and response validation fails.

Exits 0 when the spec is clean. Exits 1 when it has defects or cannot be
read or parsed.

Use '-' as <spec> to read the spec from stdin.

Examples:
  mockzilla lint openapi.yml
  mockzilla lint --format json https://example.com/openapi.json
  cat openapi.yml | mockzilla lint -`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagFormat != "text" && flagFormat != "json" {
				return fmt.Errorf("--format must be text or json, got %q", flagFormat)
			}

			specBytes, err := readSpec(args[0])
			if err != nil {
				return fmt.Errorf("reading spec: %w", err)
			}

			defects, err := lint.SpecBytes(specBytes)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			switch {
			case flagFormat == "json":
				if defects == nil {
					defects = []lint.Defect{}
				}
				if err := json.NewEncoder(out).Encode(map[string]any{"defects": defects}); err != nil {
					return err
				}
			case len(defects) == 0:
				_, _ = fmt.Fprintln(out, "No defects found")
			default:
				for _, d := range defects {
					_, _ = fmt.Fprintf(out, "%s: %s [%s]\n", d.Path, d.Detail, d.Rule)
				}
			}

			if len(defects) > 0 {
				return fmt.Errorf("%d defect(s) found", len(defects))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&flagFormat, "format", "text", `Output format: text, or json for {"defects": [{rule, path, detail}]}`)
	return cmd
}
