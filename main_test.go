package main

import (
	"strings"
	"testing"
)

func Test_wrapHelpLine_noWrapShort(t *testing.T) {
	s := "short line"
	if got := wrapHelpLine(s, 80); got != s {
		t.Errorf("got %q", got)
	}
	if got := wrapHelpLine("hi", 5); got != "hi" {
		t.Errorf("limit < 24 should return unchanged, got %q", got)
	}
}

func Test_wrapHelpLine_wrapsWhenLong(t *testing.T) {
	long := "one two three four five six seven eight nine ten eleven"
	got := wrapHelpLine(long, 24)
	if !strings.Contains(got, "\n") {
		t.Errorf("expected wrapped lines, got %q", got)
	}
}

func Test_wrapHelpLine_wrapsWords(t *testing.T) {
	// wrapHelpLine returns unchanged when limit < 24 (see help.go).
	s := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda"
	got := wrapHelpLine(s, 24)
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines: %q", got)
	}
	if strings.Contains(got, "  ") {
		t.Errorf("double space in %q", got)
	}
}
