package main

import (
	"os"

	"github.com/gek0v/milanuncios-cli/internal/cli"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	app := cli.NewApp(version)
	os.Exit(app.Execute())
}
