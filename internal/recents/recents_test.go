package recents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func Test_mergeHistory_dedupesAndCaps(t *testing.T) {
	a := Target{Cluster: "c", Service: "s", TaskARN: "arn:a", Container: "app"}
	b := Target{Cluster: "c", Service: "s", TaskARN: "arn:b", Container: "app"}
	c := Target{Cluster: "c", Service: "s", TaskARN: "arn:c", Container: "app"}
	d := Target{Cluster: "c", Service: "s", TaskARN: "arn:d", Container: "app"}

	got := mergeHistory([]Target{a, b}, a)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2 (dedupe same ARN) %+v", len(got), got)
	}
	if got[0].TaskARN != "arn:a" {
		t.Errorf("newest first: got %s", got[0].TaskARN)
	}

	got = mergeHistory([]Target{a, b, c}, d)
	if len(got) != MaxPerProfile {
		t.Fatalf("len=%d want %d %v", len(got), MaxPerProfile, got)
	}
	if got[0].TaskARN != "arn:d" || got[1].TaskARN != "arn:a" {
		t.Errorf("order: %#v", got)
	}
}

func Test_parseProfileTargets_legacyAndV2(t *testing.T) {
	legacy, _ := json.Marshal(Target{
		Cluster: "c", Service: "s", TaskARN: "arn:1", Container: "x",
	})
	got := parseProfileTargets(legacy)
	if len(got) != 1 || got[0].TaskARN != "arn:1" {
		t.Fatalf("legacy: %#v", got)
	}

	type wrap struct {
		Targets []Target `json:"targets"`
	}
	raw, _ := json.Marshal(wrap{Targets: []Target{
		{Cluster: "c", Service: "s", TaskARN: "arn:2", Container: "a"},
		{Cluster: "c", Service: "s", TaskARN: "arn:3", Container: "b"},
	}})
	got = parseProfileTargets(raw)
	if len(got) != 2 || got[0].TaskARN != "arn:2" {
		t.Fatalf("v2: %#v", got)
	}
}

func Test_roundTrip_saveLoadThree(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// filePath uses UserHomeDir — on some OS might not use HOME
	// Recents uses os.UserHomeDir() which respects HOME on Unix
	path := filepath.Join(dir, ".ecs-connect", "recents.json")

	p := "myprofile"
	t1 := Target{Cluster: "c", Service: "s", TaskARN: "arn:1", Container: "x"}
	t2 := Target{Cluster: "c", Service: "s", TaskARN: "arn:2", Container: "x"}
	t3 := Target{Cluster: "c", Service: "s", TaskARN: "arn:3", Container: "x"}
	t4 := Target{Cluster: "c", Service: "s", TaskARN: "arn:4", Container: "x"}

	_ = Save(p, t1)
	_ = Save(p, t2)
	_ = Save(p, t3)
	all, ok := LoadAll(p)
	if !ok || len(all) != 3 {
		t.Fatalf("load3: ok=%v n=%d", ok, len(all))
	}
	if all[0].TaskARN != "arn:3" {
		t.Errorf("newest arn: %s", all[0].TaskARN)
	}

	_ = Save(p, t4)
	all, _ = LoadAll(p)
	if len(all) != 3 || all[0].TaskARN != "arn:4" || all[2].TaskARN != "arn:2" {
		t.Fatalf("cap3: %#v", all)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
}
