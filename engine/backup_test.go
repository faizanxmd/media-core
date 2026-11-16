package main

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackup(t *testing.T) {
	os.MkdirAll(filepath.Join(DataDir, "tmdb"), 0o755)
	os.WriteFile(watchPath, []byte(`{"v":1}`), 0o600)
	os.WriteFile(filepath.Join(DataDir, "tmdb", "matches.json"), []byte(`{}`), 0o600)
	os.WriteFile(filepath.Join(ConfDir, "tmdb.key"), []byte("secret"), 0o600)
	dir := t.TempDir()
	day := time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC)
	out, saved, err := makeBackup(dir, day)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(out)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		names[h.Name] = true
	}
	if !names["data/watch.json"] || !names["data/tmdb/matches.json"] || len(names) != len(saved) {
		t.Fatalf("saved %v, archive %v", saved, names)
	}
	for n := range names {
		if filepath.Base(n) == "tmdb.key" || filepath.Base(n) == "aria2.conf" {
			t.Fatalf("a secret was backed up: %s", n)
		}
	}
	if fi, _ := os.Stat(out); fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode %v", fi.Mode())
	}
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, "state-2025-01-0"+string(rune('1'+i))+".tgz"), nil, 0o600)
	}
	pruneBackups(dir, 3)
	left, _ := filepath.Glob(filepath.Join(dir, "state-*.tgz"))
	if len(left) != 3 || filepath.Base(left[len(left)-1]) != "state-2026-01-02.tgz" {
		t.Fatalf("after pruning: %v", left)
	}
}
