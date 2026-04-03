package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds optional project-level settings loaded from .ecs-connect.yaml.
type Config struct {
	Profile      string        `yaml:"profile"`
	Environments []Environment `yaml:"environments"`
	DefaultSlug  string        `yaml:"default_slug"`
	Command      string        `yaml:"command"`
	Region       string        `yaml:"region"`
	Defaults     *Defaults     `yaml:"defaults"`
}

// Defaults optional shortcuts so the wizard can skip steps when values match
// (CLI flags still win when set). See NormalizeBackend for backend values.
type Defaults struct {
	Profile       string `yaml:"profile"`        // AWS profile (used if root profile is unset)
	Backend       string `yaml:"backend"`        // ecs | dynamo (aliases: dynamodb, ddb)
	Environment   string `yaml:"environment"`    // must match environments[].name when using naming
	Cluster       string `yaml:"cluster"`        // exact ECS cluster name
	Service       string `yaml:"service"`        // ECS service name or slug (naming mode)
	DynamoTable   string `yaml:"dynamo_table"`   // exact table name after keyword filter
	DynamoKeyword string `yaml:"dynamo_keyword"` // substring filter when not using naming (any string)
}

// NormalizeBackend returns "ecs", "dynamo", or "" if unset/unknown.
func NormalizeBackend(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ecs", "exec":
		return "ecs"
	case "dynamo", "dynamodb", "ddb":
		return "dynamo"
	default:
		return ""
	}
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
// directory. Returns (nil, nil) if no candidate file exists. If a file exists
// but is invalid YAML, returns an error so callers can surface it instead of
// silently ignoring the broken config.
func Discover() (*Config, error) {
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
		_, statErr := os.Stat(path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return nil, fmt.Errorf("%s: %w", path, statErr)
		}
		cfg, err := Load(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return cfg, nil
	}
	return nil, nil
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
