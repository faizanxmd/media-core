package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the engine's state paths at a throwaway folder, so no test can
// touch watch.json, jobs, the library or any other state the running engine uses. Tests that need files in
// the library make them under Media, an empty folder here.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "media-core-test-")
	if err != nil {
		panic(err)
	}
	setPaths(filepath.Join(tmp, "conf"), filepath.Join(tmp, "data"), filepath.Join(tmp, "media"))
	for _, d := range []string{JobsDir, ConfDir, Media} {
		os.MkdirAll(d, 0o755)
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}
