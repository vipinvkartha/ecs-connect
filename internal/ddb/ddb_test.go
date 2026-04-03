package ddb

import (
	"reflect"
	"testing"
)

func TestFilterTablesByKeyword(t *testing.T) {
	tables := []string{
		"accounts-production.sessions",
		"accounts-staging.sessions",
		"other",
	}
	got := FilterTablesByKeyword(tables, "staging")
	want := []string{"accounts-staging.sessions"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if len(FilterTablesByKeyword(tables, "")) != len(tables) {
		t.Error("empty keyword should keep all")
	}
}
