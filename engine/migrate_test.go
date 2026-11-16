package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withDataDir points DataDir and JobsDir at a fresh folder for one test.
func withDataDir(t *testing.T) string {
	t.Helper()
	d, j := DataDir, JobsDir
	DataDir = t.TempDir()
	JobsDir = filepath.Join(DataDir, "jobs")
	t.Cleanup(func() { DataDir, JobsDir = d, j })
	return DataDir
}

func TestMigrateFreshFolder(t *testing.T) {
	withDataDir(t)
	if err := migrateState(); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := readSchema(); n != stateSchema {
		t.Fatalf("schema %d", n)
	}
	if b, _ := filepath.Glob(filepath.Join(backupDir(), "*.tgz")); len(b) != 0 {
		t.Fatalf("a fresh folder got a backup: %v", b)
	}
}

func TestMigrateV1(t *testing.T) {
	withDataDir(t)
	os.MkdirAll(JobsDir, 0o700)
	os.WriteFile(watchPathIn(DataDir), []byte(`{}`), 0o600)
	os.WriteFile(filepath.Join(JobsDir, "a.json"), []byte(`{"id":"a","state":"starting","extra":7}`), 0o600)
	os.WriteFile(filepath.Join(JobsDir, "b.json"), []byte(`{"id":"b","state":"done"}`), 0o600)
	if err := migrateState(); err != nil {
		t.Fatal(err)
	}
	var a map[string]any
	b, _ := os.ReadFile(filepath.Join(JobsDir, "a.json"))
	json.Unmarshal(b, &a)
	if a["state"] != "queued" || a["extra"] != float64(7) {
		t.Fatalf("job a: %v", a)
	}
	if b, _ := os.ReadFile(filepath.Join(JobsDir, "b.json")); !strings.Contains(string(b), `"done"`) {
		t.Fatalf("job b changed: %s", b)
	}
	if n, _, _ := readSchema(); n != stateSchema {
		t.Fatalf("schema %d", n)
	}
	if b, _ := filepath.Glob(filepath.Join(backupDir(), "upgrade-0to1-*.tgz")); len(b) != 1 {
		t.Fatalf("no backup before migrating: %v", b)
	}
	if err := migrateState(); err != nil { // a second start changes nothing
		t.Fatal(err)
	}
}

func TestMigrateRefusesNewer(t *testing.T) {
	withDataDir(t)
	if err := writeSchema(stateSchema + 1); err != nil {
		t.Fatal(err)
	}
	err := migrateState()
	if err == nil || !strings.Contains(err.Error(), "newer version") {
		t.Fatalf("got %v", err)
	}
}

func watchPathIn(dir string) string { return filepath.Join(dir, "watch.json") }
