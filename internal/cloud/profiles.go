package cloud

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListProfiles parses ~/.aws/config and returns all available profile names.
// Respects the AWS_CONFIG_FILE environment variable.
func ListProfiles() []string {
	path := os.Getenv("AWS_CONFIG_FILE")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(home, ".aws", "config")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[profile ") && strings.HasSuffix(line, "]") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "[profile "), "]")
			name = strings.TrimSpace(name)
			if name != "" {
				seen[name] = true
			}
		} else if line == "[default]" {
			seen["default"] = true
		}
	}

	profiles := make([]string, 0, len(seen))
	for name := range seen {
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)
	return profiles
}
