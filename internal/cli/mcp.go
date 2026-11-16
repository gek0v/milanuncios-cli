package cli

import (
	"fmt"
	"strings"

	"github.com/gek0v/milanuncios-cli/internal/mcp"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *App) mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Model Context Protocol (MCP) JSON-RPC 2.0 server for AI Agents",
		Long: `Starts the stdio MCP server exposing Milanuncios tools to AI Agents (Claude Code, Cursor, Codex).
Each line over stdin is a JSON-RPC 2.0 request; responses are emitted line-by-line over stdout.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			eng := a.newWatchEngine(st)
			srv := mcp.NewServer(a.Version, a.Client, st, eng, a.Stdin, a.Stdout, a.Stderr)

			return srv.Run(cmd.Context())
		},
	}

	cmd.AddCommand(a.mcpInstallCmd())

	return cmd
}

func (a *App) mcpInstallCmd() *cobra.Command {
	var binPath string

	cmd := &cobra.Command{
		Use:   "install <claude-code|cursor|codex>",
		Short: "Configure an AI coding assistant environment with milanuncios MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			res, err := mcp.InstallTarget(target, binPath)
			if err != nil {
				return err
			}

			return a.Printer.Print(res)
		},
	}

	cmd.Flags().StringVar(&binPath, "bin", "", "custom executable path for milanuncios binary")

	return cmd
}

func (a *App) skillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Export documentation and prompt guidelines for AI Agents",
	}

	cmd.AddCommand(a.skillsGetCmd())

	return cmd
}

func (a *App) skillsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <skill-name>",
		Short: "Display skill prompt (e.g. milanuncios-usage)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(args[0])
			if name != "milanuncios-usage" && name != "usage" && name != "default" {
				return output.NotFoundf("skill %q not found (available: milanuncios-usage)", args[0])
			}

			skillText := mcp.GetSkill()
			fmt.Fprint(a.Stdout, skillText)
			return nil
		},
	}
}
