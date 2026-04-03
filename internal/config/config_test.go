package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeBackend(t *testing.T) {
	tests := []struct{ in, want string }{
		{"ecs", "ecs"},
		{"ECS", "ecs"},
		{"exec", "ecs"},
		{"dynamo", "dynamo"},
		{"DynamoDB", "dynamo"},
		{"ddb", "dynamo"},
		{"", ""},
		{"  ", ""},
		{"lambda", ""},
	}
	for _, tt := range tests {
		if got := NormalizeBackend(tt.in); got != tt.want {
			t.Errorf("NormalizeBackend(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLoad_defaultsSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	content := `
profile: p1
defaults:
  profile: p-from-defaults
  backend: dynamodb
  environment: staging
  cluster: cl1
  service: svc1
  dynamo_table: Tbl-staging
  dynamo_keyword: staging
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults == nil {
		t.Fatal("expected Defaults")
	}
	d := cfg.Defaults
	if d.Profile != "p-from-defaults" || NormalizeBackend(d.Backend) != "dynamo" ||
		d.Environment != "staging" || d.Cluster != "cl1" || d.Service != "svc1" ||
		d.DynamoTable != "Tbl-staging" || d.DynamoKeyword != "staging" {
		t.Fatalf("Defaults = %+v", d)
	}
}

func TestLoad_validYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	content := `
profile: prod
default_slug: api
command: /bin/bash
region: eu-west-1
environments:
  - name: staging
  - name: production
    confirm: true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Profile != "prod" {
		t.Errorf("Profile = %q", cfg.Profile)
	}
	if cfg.GetDefaultSlug() != "api" {
		t.Errorf("GetDefaultSlug = %q", cfg.GetDefaultSlug())
	}
	if cfg.Command != "/bin/bash" || cfg.Region != "eu-west-1" {
		t.Errorf("Command/Region = %q / %q", cfg.Command, cfg.Region)
	}
	if len(cfg.Environments) != 2 {
		t.Fatalf("Environments len = %d", len(cfg.Environments))
	}
	if cfg.Environments[0].Name != "staging" || cfg.Environments[0].Confirm {
		t.Errorf("env[0] = %+v", cfg.Environments[0])
	}
	if cfg.Environments[1].Name != "production" || !cfg.Environments[1].Confirm {
		t.Errorf("env[1] = %+v", cfg.Environments[1])
	}
}

func TestLoad_missingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad_invalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("environments: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestConfig_HasNaming(t *testing.T) {
	if (*Config)(nil).HasNaming() {
		t.Error("nil HasNaming should be false")
	}
	if (&Config{}).HasNaming() {
		t.Error("empty HasNaming should be false")
	}
	if !(&Config{Environments: []Environment{{Name: "x"}}}).HasNaming() {
		t.Error("with envs should be true")
	}
}

func TestConfig_ConfirmEnv(t *testing.T) {
	if (*Config)(nil).ConfirmEnv("production") {
		t.Error("nil ConfirmEnv")
	}
	cfg := &Config{Environments: []Environment{
		{Name: "staging", Confirm: false},
		{Name: "production", Confirm: true},
	}}
	if cfg.ConfirmEnv("staging") {
		t.Error("staging should not confirm")
	}
	if !cfg.ConfirmEnv("production") {
		t.Error("production should confirm")
	}
	if cfg.ConfirmEnv("missing") {
		t.Error("unknown env")
	}
}

func TestConfig_GetDefaultSlug(t *testing.T) {
	if (*Config)(nil).GetDefaultSlug() != "web" {
		t.Errorf("nil default slug")
	}
	if (&Config{}).GetDefaultSlug() != "web" {
		t.Errorf("empty default slug")
	}
	if (&Config{DefaultSlug: "main"}).GetDefaultSlug() != "main" {
		t.Errorf("custom slug")
	}
}

func TestDiscover_prefersCwdOverHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, ".ecs-connect.yaml"), []byte("profile: from-cwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ecs-connect.yaml"), []byte("profile: from-home\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Chdir(wd)

	cfg, err := Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if cfg == nil {
		t.Fatal("Discover returned nil")
	}
	if cfg.Profile != "from-cwd" {
		t.Errorf("Profile = %q (want cwd file first)", cfg.Profile)
	}
}

func TestDiscover_fallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd := t.TempDir()
	t.Chdir(wd)

	if err := os.WriteFile(filepath.Join(home, ".ecs-connect.yml"), []byte("profile: only-home\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Discover()
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.Profile != "only-home" {
		t.Fatalf("Discover = %#v", cfg)
	}
}

func TestDiscover_prefersYamlOverYmlInCwd(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)
	if err := os.WriteFile(filepath.Join(wd, ".ecs-connect.yml"), []byte("profile: from-yml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, ".ecs-connect.yaml"), []byte("profile: from-yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Discover()
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.Profile != "from-yaml" {
		t.Fatalf("got %#v", cfg)
	}
}

func TestDiscover_ymlExtensionInCwd(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)
	if err := os.WriteFile(filepath.Join(wd, ".ecs-connect.yml"), []byte("profile: yml-ext\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Discover()
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.Profile != "yml-ext" {
		t.Fatalf("got %#v", cfg)
	}
}

func TestDiscover_noneFound(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := Discover()
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatal("expected nil when no config files")
	}
}

func TestLoad_repoExampleYAML(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	path := filepath.Join(repoRoot, "ecs-connect.example.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Skip("ecs-connect.example.yaml not in repo root")
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load example: %v", err)
	}
	if cfg.Profile == "" || cfg.Region == "" || len(cfg.Environments) < 1 {
		t.Fatalf("unexpected parse: %+v", cfg)
	}
	if cfg.Defaults == nil || NormalizeBackend(cfg.Defaults.Backend) == "" {
		t.Fatalf("expected defaults with backend: %+v", cfg.Defaults)
	}
}

func TestDiscover_invalidYAML_returnsError(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)
	path := filepath.Join(wd, ".ecs-connect.yaml")
	// Broken structure: `defaults` indented under `region` while region is a string.
	if err := os.WriteFile(path, []byte("profile: p\nregion: eu-west-1\n defaults:\n   backend: ecs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Discover()
	if err == nil {
		t.Fatalf("expected error, got cfg=%#v", cfg)
	}
	if cfg != nil {
		t.Fatalf("cfg = %#v", cfg)
	}
}
