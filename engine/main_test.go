package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the engine's state paths at a throwaway folder, so no test can
// touch watch.json, jobs or any other state the running engine uses. Media stays:
// older tests read the real library (read-only); tests that write set their own.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "media-core-test-")
	if err != nil {
		panic(err)
	}
	setPaths(filepath.Join(tmp, "conf"), filepath.Join(tmp, "data"), Media)
	for _, d := range []string{JobsDir, ConfDir} {
		os.MkdirAll(d, 0o755)
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}
