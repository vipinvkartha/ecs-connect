package recents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Target is the last successful ECS exec target for a profile.
type Target struct {
	Cluster     string `json:"cluster"`
	Service     string `json:"service"`
	TaskARN     string `json:"task_arn"`
	TaskShortID string `json:"task_short_id"`
	Container   string `json:"container"`
	Environment string `json:"environment,omitempty"`
	AppGroup    string `json:"app_group,omitempty"`
	Slug        string `json:"slug,omitempty"`
}

type store struct {
	ByProfile map[string]Target `json:"by_profile"`
}

var mu sync.Mutex

func filePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".ecs-connect")
	return filepath.Join(dir, "recents.json"), nil
}

// Load returns the saved target for the AWS profile (may be empty string key).
func Load(awsProfile string) (Target, bool) {
	mu.Lock()
	defer mu.Unlock()

	path, err := filePath()
	if err != nil {
		return Target{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Target{}, false
	}
	var s store
	if err := json.Unmarshal(data, &s); err != nil || s.ByProfile == nil {
		return Target{}, false
	}
	t, ok := s.ByProfile[awsProfile]
	if !ok || t.Cluster == "" || t.Service == "" || t.TaskARN == "" || t.Container == "" {
		return Target{}, false
	}
	return t, true
}

// Save persists target for the AWS profile under ~/.ecs-connect/recents.json.
func Save(awsProfile string, t Target) error {
	if t.Cluster == "" || t.Service == "" || t.TaskARN == "" || t.Container == "" {
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	path, err := filePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var s store
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if s.ByProfile == nil {
		s.ByProfile = make(map[string]Target)
	}
	s.ByProfile[awsProfile] = t

	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
