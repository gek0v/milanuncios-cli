package cli

import "github.com/spf13/cobra"

// CommandTree returns the whole command tree wired but unexecuted,
// allowing tests and documentation generators to inspect it.
func CommandTree(version string) *cobra.Command {
	app := NewApp(version)
	return app.RootCmd()
}
