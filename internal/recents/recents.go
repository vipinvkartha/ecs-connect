package recents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// MaxPerProfile is how many ECS exec targets we keep per AWS profile (newest first).
const MaxPerProfile = 3

// Target is one successful ECS exec target for a profile.
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

var mu sync.Mutex

func filePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".ecs-connect")
	return filepath.Join(dir, "recents.json"), nil
}

func targetComplete(t Target) bool {
	return t.Cluster != "" && t.Service != "" && t.TaskARN != "" && t.Container != ""
}

func filterComplete(targets []Target) []Target {
	var out []Target
	for _, t := range targets {
		if targetComplete(t) {
			out = append(out, t)
		}
	}
	return out
}

// parseProfileTargets supports:
//   - legacy: a single target object {"cluster":...}
//   - v2: {"targets":[...]}
func parseProfileTargets(raw json.RawMessage) []Target {
	if len(raw) == 0 {
		return nil
	}
	var one Target
	if err := json.Unmarshal(raw, &one); err == nil && targetComplete(one) {
		return []Target{one}
	}
	var wrap struct {
		Targets []Target `json:"targets"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && len(wrap.Targets) > 0 {
		return filterComplete(wrap.Targets)
	}
	return nil
}

func readStoreMap(path string) (map[string][]Target, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]Target{}, nil
		}
		return nil, err
	}
	var top struct {
		ByProfile map[string]json.RawMessage `json:"by_profile"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}
	if top.ByProfile == nil {
		return map[string][]Target{}, nil
	}
	out := make(map[string][]Target)
	for k, v := range top.ByProfile {
		if t := parseProfileTargets(v); len(t) > 0 {
			out[k] = t
		}
	}
	return out, nil
}

func writeStoreMap(path string, m map[string][]Target) error {
	type entry struct {
		Targets []Target `json:"targets"`
	}
	wrapper := struct {
		ByProfile map[string]entry `json:"by_profile"`
	}{ByProfile: make(map[string]entry)}
	for k, v := range m {
		v = filterComplete(v)
		if len(v) == 0 {
			continue
		}
		if len(v) > MaxPerProfile {
			v = v[:MaxPerProfile]
		}
		wrapper.ByProfile[k] = entry{Targets: v}
	}
	out, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func mergeHistory(existing []Target, newest Target) []Target {
	if !targetComplete(newest) {
		return filterComplete(existing)
	}
	seen := make(map[string]struct{})
	var out []Target
	for _, x := range append([]Target{newest}, existing...) {
		if !targetComplete(x) {
			continue
		}
		if _, ok := seen[x.TaskARN]; ok {
			continue
		}
		seen[x.TaskARN] = struct{}{}
		out = append(out, x)
		if len(out) >= MaxPerProfile {
			break
		}
	}
	return out
}

func loadLocked(awsProfile string) ([]Target, bool) {
	path, err := filePath()
	if err != nil {
		return nil, false
	}
	m, err := readStoreMap(path)
	if err != nil {
		return nil, false
	}
	list := filterComplete(m[awsProfile])
	if len(list) == 0 {
		return nil, false
	}
	return list, true
}

// Load returns the most recent saved target for the profile (same as LoadIndex(..., 0)).
func Load(awsProfile string) (Target, bool) {
	return LoadIndex(awsProfile, 0)
}

// LoadAll returns up to MaxPerProfile targets for the profile, newest first.
func LoadAll(awsProfile string) ([]Target, bool) {
	mu.Lock()
	defer mu.Unlock()
	list, ok := loadLocked(awsProfile)
	if !ok {
		return nil, false
	}
	out := make([]Target, len(list))
	copy(out, list)
	return out, true
}

// LoadIndex returns the i'th saved target (0 = most recent, 1 = previous, 2 = oldest kept).
func LoadIndex(awsProfile string, i int) (Target, bool) {
	mu.Lock()
	defer mu.Unlock()
	list, ok := loadLocked(awsProfile)
	if !ok || i < 0 || i >= len(list) {
		return Target{}, false
	}
	return list[i], true
}

// Save prepends a successful exec target for the profile, dedupes by task ARN, and keeps at most MaxPerProfile.
func Save(awsProfile string, t Target) error {
	if !targetComplete(t) {
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

	m, err := readStoreMap(path)
	if err != nil {
		return fmt.Errorf("read recents: %w", err)
	}
	existing := m[awsProfile]
	m[awsProfile] = mergeHistory(existing, t)
	return writeStoreMap(path, m)
}
