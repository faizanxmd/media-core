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
	DataDir = filepath.Join(tmp, "data")
	JobsDir = filepath.Join(DataDir, "jobs")
	ConfDir = filepath.Join(tmp, "conf")
	Aria2Conf = filepath.Join(ConfDir, "aria2.conf")
	introsPath = filepath.Join(DataDir, "intros.json")
	miPath = filepath.Join(DataDir, "mediainfo.json")
	tmdbDir = filepath.Join(DataDir, "tmdb")
	settingsPath = filepath.Join(DataDir, "settings.json")
	watchPath = filepath.Join(DataDir, "watch.json")
	for _, d := range []string{JobsDir, ConfDir} {
		os.MkdirAll(d, 0o755)
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}
