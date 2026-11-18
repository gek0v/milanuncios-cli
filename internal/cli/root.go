package cli

import (
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/gek0v/milanuncios-cli/internal/config"
	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
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

	Paths       config.Paths
	Cfg         *config.Config
	Profile     string
	Printer     output.Printer
	Interactive bool
	Client      *milanuncios.Client
}

// NewApp creates a new App instance with default standard I/O streams.
func NewApp(version string) *App {
	return &App{
		Version: version,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Client:  milanuncios.NewClient(version),
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
			return a.setup(cmd)
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

	root.AddCommand(
		a.authCmd(),
		a.profileCmd(),
		a.searchCmd(),
		a.categoryCmd(),
		a.adCmd(),
		a.meCmd(),
		a.chatCmd(),
		a.configCmd(),
		a.watchCmd(),
		a.sinkCmd(),
		a.mcpCmd(),
		a.skillsCmd(),
		a.favoriteCmd(),
	)

	return root
}

func (a *App) favoriteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "favorite",
		Short: "Manage local agent favorite annotations and notes",
	}

	cmd.AddCommand(
		a.favoriteTagCmd(),
		a.favoriteNotesCmd(),
	)

	return cmd
}

func (a *App) setup(cmd *cobra.Command) error {
	if cmd.Name() == "help" || cmd.Name() == "completion" || (cmd.Parent() != nil && cmd.Parent().Name() == "completion") {
		return nil
	}

	format, err := output.ParseFormat(a.FlagFormat)
	if err != nil {
		return err
	}
	if a.FlagErrorFormat != "text" && a.FlagErrorFormat != "json" {
		return output.Usagef("--error-format must be text or json, got %q", a.FlagErrorFormat)
	}

	stdoutFile, _ := a.Stdout.(*os.File)
	stdinFile, _ := a.Stdin.(*os.File)
	stderrFile, _ := a.Stderr.(*os.File)
	a.Interactive = !a.FlagNoInput && output.IsTerminal(stdinFile) && output.IsTerminal(stdoutFile) && output.IsTerminal(stderrFile)
	a.Printer = output.Printer{
		W:      a.Stdout,
		Format: format,
		Color:  output.ColorEnabled(a.FlagNoColor, stdoutFile),
	}

	a.Paths = config.DefaultPaths()
	cfg, err := config.Load(a.Paths.ConfigFile)
	if err != nil {
		return err
	}
	a.Cfg = cfg
	a.Profile = config.ResolveProfile(a.FlagProfile, a.Cfg)

	if a.Client == nil {
		a.Client = milanuncios.NewClient(a.Version)
	}
	if a.FlagDebug {
		a.Client.Debug = a.Stderr
	}
	a.Client.Notice = func(s string) {
		fmt.Fprintln(a.Stderr, "milanuncios: "+s)
	}

	// 1. Env override for CI or ephemeral automation
	if envToken := os.Getenv("MILANUNCIOS_SESSION_TOKEN"); envToken != "" {
		a.Client.SetSession(envToken)
	} else {
		// 2. Load persisted credentials for profile
		if creds, err := config.LoadCredentials(a.Paths.CredentialsFile); err == nil {
			if p, ok := creds.Profiles[a.Profile]; ok && p.SessionToken != "" {
				a.Client.SetSession(p.SessionToken)
			}
		}
	}

	if baseURL := os.Getenv("MILANUNCIOS_BASE_URL"); baseURL != "" {
		a.Client.BaseURL = baseURL
	}

	proxyStr := os.Getenv("MILANUNCIOS_PROXY")
	if proxyStr == "" && a.Cfg.Proxy != "" {
		proxyStr = a.Cfg.Proxy
	}
	if proxyStr != "" {
		u, err := url.Parse(proxyStr)
		if err != nil {
			return output.Usagef("invalid proxy configuration: %v", err)
		}
		a.Client.UseProxy(u)
	}

	return nil
}

// Execute runs the root command against the configured streams and returns the exit code.
func (a *App) Execute() int {
	cmd := a.RootCmd()
	cmd.SetIn(a.Stdin)
	cmd.SetOut(a.Stdout)
	cmd.SetErr(a.Stderr)

	executedCmd, err := cmd.ExecuteC()
	if err == nil {
		return 0
	}

	cmdPath := "milanuncios"
	if executedCmd != nil {
		cmdPath = executedCmd.CommandPath()
	}

	return output.PrintError(a.Stderr, err, output.ErrorFormat(a.FlagErrorFormat), a.Version, cmdPath)
}
