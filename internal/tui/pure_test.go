package tui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func Test_filterClusters(t *testing.T) {
	all := []string{"a-staging", "b-production", "c-staging", "d-dev", "e-staging"}
	got := filterClusters(all, "staging")
	want := []string{"a-staging", "c-staging", "e-staging"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if len(filterClusters(nil, "x")) != 0 {
		t.Error("nil input")
	}
}

func Test_extractSlugs_orderAndDedup(t *testing.T) {
	services := []string{
		"home-worker-staging",
		"home-staging",
		"home-api-staging",
		"other-prod",
		"home-api-staging",
	}
	got := extractSlugs(services, "home", "staging", "web")
	want := []string{"web", "api", "worker"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func Test_extractSlugs_noDefaultSlugStillReturnsOthers(t *testing.T) {
	services := []string{"home-worker-staging", "home-api-staging"}
	got := extractSlugs(services, "home", "staging", "web")
	if len(got) != 2 || got[0] != "api" || got[1] != "worker" {
		t.Errorf("got %v (sorted others only, no web service)", got)
	}
}

func Test_extractSlugs_empty(t *testing.T) {
	if len(extractSlugs([]string{"other"}, "home", "staging", "web")) != 0 {
		t.Error("expected no slugs")
	}
}

func Test_applyFilterWithIndices_emptyFilter(t *testing.T) {
	items := []string{"a", "b", "c"}
	f, idx := applyFilterWithIndices(items, "")
	if !reflect.DeepEqual(f, items) || !reflect.DeepEqual(idx, []int{0, 1, 2}) {
		t.Errorf("f=%v idx=%v", f, idx)
	}
}

func Test_applyFilterWithIndices_caseInsensitive(t *testing.T) {
	items := []string{"Alpha", "beta", "ALPHA"}
	f, idx := applyFilterWithIndices(items, "al")
	if len(f) != 2 || len(idx) != 2 {
		t.Fatalf("got f=%v idx=%v", f, idx)
	}
	if idx[0] != 0 || idx[1] != 2 {
		t.Errorf("indices %v", idx)
	}
}

func Test_applyFilterWithIndices_noMatch(t *testing.T) {
	f, idx := applyFilterWithIndices([]string{"x"}, "zzz")
	if len(f) != 0 || len(idx) != 0 {
		t.Errorf("f=%v idx=%v", f, idx)
	}
}

func Test_listNav(t *testing.T) {
	var cursor int
	if listNav(tea.KeyMsg{Type: tea.KeyUp}, &cursor, 3) {
		t.Error("up at 0 should not return true")
	}
	listNav(tea.KeyMsg{Type: tea.KeyDown}, &cursor, 3)
	if cursor != 1 {
		t.Errorf("cursor = %d", cursor)
	}
	if !listNav(tea.KeyMsg{Type: tea.KeyEnter}, &cursor, 3) {
		t.Error("enter should return true")
	}
}

func Test_listNav_kAndJ(t *testing.T) {
	var cursor int
	listNav(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, &cursor, 2)
	if cursor != 1 {
		t.Errorf("after j, cursor = %d", cursor)
	}
	listNav(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}, &cursor, 2)
	if cursor != 0 {
		t.Errorf("after k, cursor = %d", cursor)
	}
}
