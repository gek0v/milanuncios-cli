package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Config represents the application configuration stored in config.toml.
type Config struct {
	DefaultProfile string                   `toml:"default_profile,omitempty"`
	Proxy          string                   `toml:"proxy,omitempty"`
	Profiles       map[string]ProfileConfig `toml:"profiles,omitempty"`
	Watch          WatchConfig              `toml:"watch,omitempty"`
	Sinks          map[string]SinkConfig    `toml:"sinks,omitempty"`
}

// ProfileConfig represents per-profile configuration.
type ProfileConfig struct {
	Location *Location `toml:"location,omitempty"`
}

// Location represents location preferences for searches.
type Location struct {
	Province     string  `toml:"province,omitempty"`
	Municipality string  `toml:"municipality,omitempty"`
	Lat          float64 `toml:"lat,omitempty"`
	Lng          float64 `toml:"lng,omitempty"`
	RadiusKm     int     `toml:"radius_km,omitempty"`
}

// WatchConfig represents watch engine settings.
type WatchConfig struct {
	Interval string `toml:"interval,omitempty"`
}

// SinkConfig represents an external notification target.
type SinkConfig struct {
	Type      string   `toml:"type"`
	URL       string   `toml:"url,omitempty"`
	Topic     string   `toml:"topic,omitempty"`
	Template  string   `toml:"template,omitempty"`
	Command   []string `toml:"command,omitempty"`
	TokenFile string   `toml:"token_file,omitempty"`
}

// New creates an initialized empty Config.
func New() *Config {
	return &Config{
		DefaultProfile: "default",
		Profiles:       make(map[string]ProfileConfig),
		Sinks:          make(map[string]SinkConfig),
	}
}

// Load reads and parses config.toml. If the file does not exist, an empty Config is returned.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return New(), nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	cfg := New()
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]ProfileConfig)
	}
	if cfg.Sinks == nil {
		cfg.Sinks = make(map[string]SinkConfig)
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = "default"
	}

	return cfg, nil
}

// Save serializes the configuration to disk with strict 0600 permissions.
func Save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("serializing config: %w", err)
	}

	// 0600: read/write for owner only
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// ResolveProfile resolves which profile should be active:
// 1. Explicit CLI flag or environment variable
// 2. Configured default profile
// 3. Fallback "default"
func ResolveProfile(flag string, cfg *Config) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("MILANUNCIOS_PROFILE"); env != "" {
		return env
	}
	if cfg != nil && cfg.DefaultProfile != "" {
		return cfg.DefaultProfile
	}
	return "default"
}

// Get reads a property by its dot-separated key.
func (c *Config) Get(key string) (any, error) {
	parts := strings.Split(key, ".")
	switch parts[0] {
	case "default_profile":
		return c.DefaultProfile, nil
	case "proxy":
		return c.Proxy, nil
	case "watch":
		if len(parts) > 1 && parts[1] == "interval" {
			return c.Watch.Interval, nil
		}
		return c.Watch, nil
	case "profiles":
		if len(parts) < 2 {
			return c.Profiles, nil
		}
		p, ok := c.Profiles[parts[1]]
		if !ok {
			return nil, fmt.Errorf("profile %q not found", parts[1])
		}
		if len(parts) >= 3 && parts[2] == "location" {
			if p.Location == nil {
				return nil, nil
			}
			if len(parts) == 4 {
				switch parts[3] {
				case "province":
					return p.Location.Province, nil
				case "municipality":
					return p.Location.Municipality, nil
				case "lat":
					return p.Location.Lat, nil
				case "lng":
					return p.Location.Lng, nil
				case "radius_km":
					return p.Location.RadiusKm, nil
				}
			}
			return p.Location, nil
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unknown config key %q", key)
	}
}

// Set updates a property by its dot-separated key.
func (c *Config) Set(key, value string) error {
	parts := strings.Split(key, ".")
	switch parts[0] {
	case "default_profile":
		c.DefaultProfile = value
		return nil
	case "proxy":
		c.Proxy = value
		return nil
	case "watch":
		if len(parts) == 2 && parts[1] == "interval" {
			c.Watch.Interval = value
			return nil
		}
		return fmt.Errorf("invalid watch key %q (did you mean watch.interval?)", key)
	case "profiles":
		if len(parts) < 3 {
			return fmt.Errorf("incomplete profile key %q", key)
		}
		profName := parts[1]
		prof := c.Profiles[profName]
		if prof.Location == nil {
			prof.Location = &Location{}
		}

		if parts[2] == "location" && len(parts) == 4 {
			switch parts[3] {
			case "province":
				prof.Location.Province = value
			case "municipality":
				prof.Location.Municipality = value
			case "lat":
				f, err := strconv.ParseFloat(value, 64)
				if err != nil {
					return fmt.Errorf("invalid lat float %q", value)
				}
				prof.Location.Lat = f
			case "lng":
				f, err := strconv.ParseFloat(value, 64)
				if err != nil {
					return fmt.Errorf("invalid lng float %q", value)
				}
				prof.Location.Lng = f
			case "radius_km":
				i, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid radius_km integer %q", value)
				}
				prof.Location.RadiusKm = i
			default:
				return fmt.Errorf("unknown location field %q", parts[3])
			}
			c.Profiles[profName] = prof
			return nil
		}
		return fmt.Errorf("unknown profile key %q", key)
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
}
