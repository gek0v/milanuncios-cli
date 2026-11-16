package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// App holds dependencies and global state for the CLI commands.
type App struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer

	FlagProfile     string
	FlagFormat      string
	FlagNoColor     bool
	FlagErrorFormat string
	FlagNoInput     bool
	FlagDebug       bool
}

// NewApp creates a new App instance with default standard I/O streams.
func NewApp(version string) *App {
	return &App{
		Version: version,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}
}

// RootCmd constructs and wires the root milanuncios command.
func (a *App) RootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "milanuncios",
		Short: "Terminal client for the Milanuncios marketplace",
		Long: `milanuncios is an unofficial terminal client for Milanuncios.

Built for both human terminal usage and autonomous AI Agents (MCP integration).
Output is JSON on stdout by default so every command pipes cleanly into jq.
Use --format pretty for human-friendly tabular output.
Errors go to stderr; exit codes follow the semantic contract documented in docs/.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       a.Version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return a.validateGlobalFlags()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	root.SetVersionTemplate("milanuncios {{.Version}}\n")
	root.CompletionOptions.HiddenDefaultCmd = false

	pf := root.PersistentFlags()
	pf.StringVar(&a.FlagProfile, "profile", "", "profile to act as (env MILANUNCIOS_PROFILE)")
	pf.StringVar(&a.FlagFormat, "format", "json", "output format: json, jsonl, pretty, toon")
	pf.BoolVar(&a.FlagNoColor, "no-color", false, "disable colour in pretty output (also NO_COLOR)")
	pf.StringVar(&a.FlagErrorFormat, "error-format", "text", "error rendering on stderr: text or json")
	pf.BoolVar(&a.FlagNoInput, "no-input", false, "never prompt; fail when input is missing")
	pf.BoolVar(&a.FlagDebug, "debug", false, "log every http request to stderr (secrets redacted)")

	_ = root.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"json", "jsonl", "pretty", "toon"}, cobra.ShellCompDirectiveNoFileComp))
	_ = root.RegisterFlagCompletionFunc("error-format", cobra.FixedCompletions([]string{"text", "json"}, cobra.ShellCompDirectiveNoFileComp))

	return root
}

func (a *App) validateGlobalFlags() error {
	switch a.FlagFormat {
	case "json", "jsonl", "pretty", "toon":
	default:
		return fmt.Errorf("unknown format %q. Use json, jsonl, pretty or toon", a.FlagFormat)
	}

	switch a.FlagErrorFormat {
	case "text", "json":
	default:
		return fmt.Errorf("--error-format must be text or json, got %q", a.FlagErrorFormat)
	}

	return nil
}

// Execute runs the root command against the configured streams and returns the exit code.
func (a *App) Execute() int {
	cmd := a.RootCmd()
	cmd.SetIn(a.Stdin)
	cmd.SetOut(a.Stdout)
	cmd.SetErr(a.Stderr)

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(a.Stderr, "milanuncios: %s\n", err.Error())
		return 1
	}
	return 0
}
