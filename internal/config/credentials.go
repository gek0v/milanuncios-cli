package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// ProfileCredentials holds authenticated session references for one profile.
type ProfileCredentials struct {
	SessionToken string `toml:"session_token,omitempty"`
	UserHash     string `toml:"user_hash,omitempty"`
	Email        string `toml:"email,omitempty"`
	Name         string `toml:"name,omitempty"`
	UpdatedAt    string `toml:"updated_at,omitempty"`
}

// Credentials holds credentials for all profiles stored securely in credentials.toml.
type Credentials struct {
	Profiles map[string]ProfileCredentials `toml:"profiles,omitempty"`
}

// NewCredentials creates an empty Credentials structure.
func NewCredentials() *Credentials {
	return &Credentials{
		Profiles: make(map[string]ProfileCredentials),
	}
}

// LoadCredentials reads credentials.toml. If the file does not exist, an empty structure is returned.
func LoadCredentials(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewCredentials(), nil
		}
		return nil, fmt.Errorf("reading credentials %s: %w", path, err)
	}

	creds := NewCredentials()
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(creds); err != nil {
		return nil, fmt.Errorf("parsing credentials %s: %w", path, err)
	}

	if creds.Profiles == nil {
		creds.Profiles = make(map[string]ProfileCredentials)
	}
	return creds, nil
}

// SaveCredentials writes credentials to disk with strict 0600 file permissions.
func SaveCredentials(path string, creds *Credentials) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating credentials directory: %w", err)
	}

	data, err := toml.Marshal(creds)
	if err != nil {
		return fmt.Errorf("serializing credentials: %w", err)
	}

	// 0600: read/write for owner only
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing credentials file: %w", err)
	}

	return nil
}

// RemoveProfileCredentials deletes credentials for a profile from disk.
func RemoveProfileCredentials(path string, profile string) error {
	creds, err := LoadCredentials(path)
	if err != nil {
		return err
	}
	if _, ok := creds.Profiles[profile]; !ok {
		return nil
	}
	delete(creds.Profiles, profile)
	return SaveCredentials(path, creds)
}
