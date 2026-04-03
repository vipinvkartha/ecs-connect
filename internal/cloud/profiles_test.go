package cloud

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListProfiles_emptyOnMissingFile(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "no-such-config"))
	if p := ListProfiles(); len(p) != 0 {
		t.Fatalf("expected empty, got %v", p)
	}
}

func TestListProfiles_parsesProfilesAndDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `
# comment
[default]
region = us-east-1

[profile alpha]
region = eu-west-1

[profile beta]
region = ap-south-1

[profile spaced name ]
region = x

`

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_CONFIG_FILE", path)

	got := ListProfiles()
	want := []string{"alpha", "beta", "default", "spaced name"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q; full %v", i, got[i], want[i], got)
			break
		}
	}
}

func TestListProfiles_ignoresMalformedSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(`
[profile ]
[profile ok]
region = x
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_CONFIG_FILE", path)
	got := ListProfiles()
	if len(got) != 1 || got[0] != "ok" {
		t.Fatalf("got %v", got)
	}
}
