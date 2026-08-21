package main

import "testing"

func TestResolvedVersionPrefersInjectedVersion(t *testing.T) {
	original := version
	version = "v0.1.0"
	t.Cleanup(func() { version = original })
	if got := resolvedVersion(); got != "v0.1.0" {
		t.Fatalf("resolvedVersion=%q", got)
	}
}
