package config

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// Paths holds the resolved filesystem paths used by the application.
type Paths struct {
	ConfigFile      string
	CredentialsFile string
	StateDB         string
}

// DefaultPaths resolves the XDG paths for milanuncios-cli.
func DefaultPaths() Paths {
	configFile := os.Getenv("MILANUNCIOS_CONFIG")
	if configFile == "" {
		configFile = filepath.Join(xdg.ConfigHome, "milanuncios-cli", "config.toml")
	}

	return Paths{
		ConfigFile:      configFile,
		CredentialsFile: filepath.Join(xdg.ConfigHome, "milanuncios-cli", "credentials.toml"),
		StateDB:         filepath.Join(xdg.DataHome, "milanuncios-cli", "state.db"),
	}
}
