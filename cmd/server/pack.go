package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mockzilla/mockzilla/v2/pkg/pack"
	"github.com/spf13/cobra"
)

func packCommand() *cobra.Command {
	var (
		flagOutput      string
		flagName        string
		flagDescription string
		flagMinVersion  string
		flagSkipGit     bool
	)

	cmd := &cobra.Command{
		Use:   "pack [flags] <dir>",
		Short: "Pack a service directory into a .mockz archive",
		Long: `Pack discovers services in <dir> and writes a .mockz archive
that can be served with 'mockzilla <archive.mockz>'.

The archive carries a .mockzilla.json manifest declaring every
service's name, mount, mode (spec/static/merge), and file paths so the
runtime can register services without re-walking the tree.

By default the output file is written next to <dir>, named
'<dir-basename>.mockz'. Override with --output.

Auto-detected git metadata (remote, ref, commit) is embedded when
<dir> is inside a git working tree. Disable with --skip-git.

Examples:
  mockzilla pack ./
  mockzilla pack --output mocks.mockz ./
  mockzilla pack --name my-mocks --description "QA mocks" ./
  mockzilla pack --min-version 2.3.0 ./
  mockzilla pack --skip-git ./`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			srcDir := args[0]
			info, err := os.Stat(srcDir)
			if err != nil {
				return fmt.Errorf("stat %s: %w", srcDir, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory", srcDir)
			}

			outPath := flagOutput
			if outPath == "" {
				abs, _ := filepath.Abs(srcDir)
				outPath = filepath.Join(filepath.Dir(abs), filepath.Base(abs)+".mockz")
			}

			opts := pack.Options{
				Name:                flagName,
				Description:         flagDescription,
				MinMockzillaVersion: flagMinVersion,
				CreatedBy:           "mockzilla/" + version,
				SkipGitSource:       flagSkipGit,
			}
			if err := pack.Pack(srcDir, outPath, opts); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Packed %s -> %s\n", srcDir, outPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&flagOutput, "output", "o", "",
		"Output .mockz path (default: <basename>.mockz next to <dir>)")
	cmd.Flags().StringVar(&flagName, "name", "", "Display name embedded in the manifest")
	cmd.Flags().StringVar(&flagDescription, "description", "", "Free-text description in the manifest")
	cmd.Flags().StringVar(&flagMinVersion, "min-version", "",
		"Minimum mockzilla version required to load this archive")
	cmd.Flags().BoolVar(&flagSkipGit, "skip-git", false,
		"Don't embed git remote/ref/commit in the manifest")
	return cmd
}
