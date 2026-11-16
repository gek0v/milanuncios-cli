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

	credFile := os.Getenv("MILANUNCIOS_CREDENTIALS_FILE")
	if credFile == "" {
		credFile = filepath.Join(xdg.ConfigHome, "milanuncios-cli", "credentials.toml")
	}

	stateDB := os.Getenv("MILANUNCIOS_STATE_DB")
	if stateDB == "" {
		stateDB = filepath.Join(xdg.DataHome, "milanuncios-cli", "state.db")
	}

	return Paths{
		ConfigFile:      configFile,
		CredentialsFile: credFile,
		StateDB:         stateDB,
	}
}
