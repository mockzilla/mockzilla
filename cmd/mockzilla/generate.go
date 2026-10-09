package main

import (
	"context"
	"io"
	"os"
	"os/signal"

	"github.com/mockzilla/mockzilla-codegen/pkg/cli"
)

// runGenerate runs `mockzilla generate`, the generate command of mockzilla-codegen: same flags,
// same codegen.yaml. args start with "generate".
func runGenerate(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return (&cli.Command{Name: "mockzilla", Stdout: stdout, Stderr: stderr}).Run(ctx, args)
}
