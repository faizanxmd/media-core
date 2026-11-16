package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A direct download aimed at a file already in the library is refused: aria2
// runs with continue=true and would append to it.
func TestNewJobRefusesExistingLibraryFile(t *testing.T) {
	oldM := Media
	Media = t.TempDir()
	defer func() { Media = oldM }()
	plan := Plan{Kind: "movie", Title: "Film", Year: "2020", Ext: "mkv"}
	dir, fn, _, err := planPaths(plan)
	if err != nil || fn == "" {
		t.Fatalf("planPaths: %q %v", fn, err)
	}
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, fn), []byte("the film"), 0o644)
	e := NewEngine(&Store{jobs: map[string]*Job{}, dirty: map[string]bool{}, subs: map[chan struct{}]bool{}}, nil, &settingsStore{})
	_, err = e.NewJob(Probe{URL: "http://x/f.mkv", Filename: "f.mkv"}, plan)
	if err == nil || !strings.Contains(err.Error(), "already in the library") {
		t.Fatalf("want refusal, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, fn)); string(b) != "the film" {
		t.Fatal("library file changed")
	}
}
