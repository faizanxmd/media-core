package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLibraryTargetRefusesUnsafePaths(t *testing.T) {
	for _, p := range []string{"", "Movies", "TV Shows", "Music", "/", "..", "../etc/passwd",
		"Movies/../../etc", "Movies/X/.incoming", "Other/X", "Movies/.convert/x"} {
		if abs, _, err := libraryTarget(p); err == nil {
			t.Errorf("%q was accepted (%s)", p, abs)
		}
	}
	// an existing item passes and stays inside the library
	name := "Example (2020)"
	if err := os.MkdirAll(filepath.Join(Media, "Movies", name), 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Join(Media, "Movies"))
	if abs, sec, err := libraryTarget("Movies/" + name); err != nil || sec != "Movies" ||
		abs != filepath.Join(Media, "Movies", name) {
		t.Errorf("%s: %s %s %v", name, abs, sec, err)
	}
	// ".." that collapses back inside is just a normal path
	if abs, _, err := libraryTarget("Movies/x/../" + name); err != nil || abs != filepath.Join(Media, "Movies", name) {
		t.Errorf("collapsed path: %s %v", abs, err)
	}
}
