package config

import (
	"os"
	"path/filepath"
	"testing"
)

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

	cfg := Discover()
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

	cfg := Discover()
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
	cfg := Discover()
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
	cfg := Discover()
	if cfg == nil || cfg.Profile != "yml-ext" {
		t.Fatalf("got %#v", cfg)
	}
}

func TestDiscover_noneFound(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if Discover() != nil {
		t.Fatal("expected nil when no config files")
	}
}
