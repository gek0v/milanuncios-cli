package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gek0v/milanuncios-cli/internal/cli"
	"github.com/spf13/cobra/doc"
)

func main() {
	outDir := filepath.Join("docs", "commands")
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create docs directory: %v\n", err)
		os.Exit(1)
	}

	app := cli.NewApp("dev")
	rootCmd := app.RootCmd()

	// Disable auto-generated timestamps for deterministic git diffs
	rootCmd.DisableAutoGenTag = true

	if err := doc.GenMarkdownTree(rootCmd, outDir); err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate markdown documentation: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully generated CLI command documentation in %s/\n", outDir)
}
