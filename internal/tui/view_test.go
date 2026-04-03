package tui

import (
	"strings"
	"testing"
	"time"

	"ecs-connect/internal/cloud"
)

func Test_visibleWindow(t *testing.T) {
	tests := []struct {
		total, cursor, maxV, wantStart, wantEnd int
	}{
		{3, 0, 10, 0, 3},
		{5, 2, 3, 1, 4},
		{100, 0, 5, 0, 5},
		{100, 50, 5, 48, 53},
		{100, 99, 5, 95, 100},
	}
	for _, tt := range tests {
		s, e := visibleWindow(tt.total, tt.cursor, tt.maxV)
		if s != tt.wantStart || e != tt.wantEnd {
			t.Errorf("visibleWindow(%d,%d,%d) = (%d,%d), want (%d,%d)",
				tt.total, tt.cursor, tt.maxV, s, e, tt.wantStart, tt.wantEnd)
		}
	}
}

func Test_taskLabels(t *testing.T) {
	tasks := []cloud.TaskInfo{
		{
			ShortID:   "abc",
			Status:    "RUNNING",
			CreatedAt: time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		},
		{ShortID: "def", Status: "PENDING", CreatedAt: time.Time{}},
	}
	labels := taskLabels(tasks)
	if len(labels) != 2 {
		t.Fatal(labels)
	}
	if !strings.Contains(labels[0], "abc") || !strings.Contains(labels[0], "RUNNING") {
		t.Errorf("labels[0] = %q", labels[0])
	}
	if !strings.Contains(labels[0], "2024-06-01") {
		t.Errorf("expected date in label: %q", labels[0])
	}
	if !strings.Contains(labels[1], "def") || !strings.Contains(labels[1], "PENDING") {
		t.Errorf("labels[1] = %q", labels[1])
	}
}

func Test_relativeTime(t *testing.T) {
	if relativeTime(time.Time{}) != "" {
		t.Error("zero time")
	}
	now := time.Now()
	if got := relativeTime(now.Add(-30 * time.Second)); got != "now" {
		t.Errorf("30s ago = %q", got)
	}
	if got := relativeTime(now.Add(-5 * time.Minute)); got != "5m" {
		t.Errorf("5m ago = %q", got)
	}
	if got := relativeTime(now.Add(-3 * time.Hour)); got != "3h" {
		t.Errorf("3h ago = %q", got)
	}
	if got := relativeTime(now.Add(-50 * time.Hour)); got != "2d" {
		t.Errorf("50h ago = %q", got)
	}
}
