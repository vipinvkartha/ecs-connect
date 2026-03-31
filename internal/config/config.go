package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds optional project-level settings loaded from .ecs-connect.yaml.
type Config struct {
	Environments []Environment `yaml:"environments"`
	DefaultSlug  string        `yaml:"default_slug"`
	Command      string        `yaml:"command"`
	Region       string        `yaml:"region"`
}

// Environment defines one selectable environment and whether it requires
// confirmation before connecting.
type Environment struct {
	Name    string `yaml:"name"`
	Confirm bool   `yaml:"confirm"`
}

// Load reads and parses a config file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Discover searches for a config file in the working directory and home
// directory. Returns nil if no config file is found.
func Discover() *Config {
	candidates := []string{
		".ecs-connect.yaml",
		".ecs-connect.yml",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".ecs-connect.yaml"),
			filepath.Join(home, ".ecs-connect.yml"),
		)
	}
	for _, path := range candidates {
		if cfg, err := Load(path); err == nil {
			return cfg
		}
	}
	return nil
}

// HasNaming reports whether the config defines environment-based naming.
// Safe to call on a nil receiver.
func (c *Config) HasNaming() bool {
	return c != nil && len(c.Environments) > 0
}

// ConfirmEnv reports whether the given environment requires confirmation.
// Safe to call on a nil receiver.
func (c *Config) ConfirmEnv(env string) bool {
	if c == nil {
		return false
	}
	for _, e := range c.Environments {
		if e.Name == env && e.Confirm {
			return true
		}
	}
	return false
}

// GetDefaultSlug returns the configured default slug, falling back to "web".
// Safe to call on a nil receiver.
func (c *Config) GetDefaultSlug() string {
	if c != nil && c.DefaultSlug != "" {
		return c.DefaultSlug
	}
	return "web"
}
