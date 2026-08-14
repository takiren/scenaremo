package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/takiren/scenaremo/internal/project"
)

var runEject = project.Eject

func newEjectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eject <ディレクトリ>",
		Short: "動画専用の renderer を切り出す",
		Long: "動画ディレクトリの中に共有 renderer をコピーして独立させます。\n" +
			"eject すると、その動画の合成には切り出された専用の renderer が優先して使われるようになります。",
		Example:               "  scenaremo eject videos/ep01",
		Args:                  exactlyOneEjectDir,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := runEject(args[0])
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), res.RendererDir); err != nil {
				return err
			}
			printEjectSummary(cmd.ErrOrStderr(), res)
			return nil
		},
	}

	return cmd
}

func exactlyOneEjectDir(cmd *cobra.Command, args []string) error {
	switch {
	case len(args) == 0:
		return &usageError{fmt.Errorf("切り出す動画ディレクトリを指定してください (例: %s videos/ep01)", cmd.CommandPath())}
	case len(args) > 1:
		return &usageError{fmt.Errorf("ディレクトリは1つだけ指定してください: %s", strings.Join(args, " "))}
	}
	return nil
}

func printEjectSummary(w io.Writer, res *project.EjectResult) {
	_, _ = fmt.Fprintf(w, "%s に renderer/ を切り出しました\n", res.Dir)
	for _, path := range res.Created {
		_, _ = fmt.Fprintf(w, "  %s\n", path)
	}
	_, _ = fmt.Fprintf(w, "\n次の一手:\n  cd %s && pnpm install\n", res.RendererDir)
}
