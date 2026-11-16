package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tarNames(t *testing.T, p string) map[string]bool {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
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
	return names
}

func writeTar(t *testing.T, p string, files map[string]string) {
	t.Helper()
	f, _ := os.Create(p)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for n, b := range files {
		tw.WriteHeader(&tar.Header{Name: n, Mode: 0o600, Size: int64(len(b)), Typeflag: tar.TypeReg})
		tw.Write([]byte(b))
	}
	tw.Close()
	gz.Close()
	f.Close()
}

func TestBackupAndRestore(t *testing.T) {
	os.MkdirAll(filepath.Join(DataDir, "tmdb"), 0o700)
	os.WriteFile(watchPath, []byte(`{"v":1}`), 0o600)
	os.WriteFile(filepath.Join(DataDir, "tmdb", "matches.json"), []byte(`{}`), 0o600)
	os.WriteFile(filepath.Join(ConfDir, "tmdb.key"), []byte("secret"), 0o600)
	os.WriteFile(filepath.Join(ConfDir, "devices.json"), []byte(`{"v":1,"devices":[]}`), 0o600)
	t.Cleanup(func() {
		os.Remove(filepath.Join(ConfDir, "devices.json"))
		os.Remove(filepath.Join(ConfDir, "tmdb.key"))
	})
	out := filepath.Join(t.TempDir(), "state-2026-01-02.tgz")
	saved, err := writeBackup(out)
	if err != nil {
		t.Fatal(err)
	}
	names := tarNames(t, out)
	if !names["manifest.json"] || !names["data/watch.json"] || !names["data/tmdb/matches.json"] || !names["conf/devices.json"] || len(names) != len(saved)+1 {
		t.Fatalf("saved %v, archive %v", saved, names)
	}
	for n := range names {
		if strings.Contains(n, "tmdb.key") || strings.Contains(n, "aria2.conf") || strings.Contains(n, "cli.token") {
			t.Fatalf("a secret was backed up: %s", n)
		}
	}
	if fi, _ := os.Stat(out); fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode %v", fi.Mode())
	}

	files, man, err := readBackup(out)
	if err != nil || man == nil || man.Schema != stateSchema {
		t.Fatalf("read back: %v %+v", err, man)
	}
	if string(files["data/watch.json"]) != `{"v":1}` {
		t.Fatalf("watch.json came back as %q", files["data/watch.json"])
	}
}

func TestReadBackupRefuses(t *testing.T) {
	dir := t.TempDir()
	evil := filepath.Join(dir, "evil.tgz")
	writeTar(t, evil, map[string]string{"../../.bashrc": "x"})
	if _, _, err := readBackup(evil); err == nil {
		t.Fatal("restore accepted a path outside the state")
	}
	newer := filepath.Join(dir, "newer.tgz")
	m, _ := json.Marshal(backupManifest{Schema: stateSchema + 1})
	writeTar(t, newer, map[string]string{"manifest.json": string(m)})
	if _, _, err := readBackup(newer); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("a newer backup: %v", err)
	}
	bad := filepath.Join(dir, "bad.tgz")
	m, _ = json.Marshal(backupManifest{Schema: 1, Files: []manifestFile{{"data/watch.json", 2, strings.Repeat("0", 64)}}})
	writeTar(t, bad, map[string]string{"manifest.json": string(m), "data/watch.json": "{}"})
	if _, _, err := readBackup(bad); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("a damaged file: %v", err)
	}
	// The old shell script's archive: flat names, no manifest.
	old := filepath.Join(dir, "old.tgz")
	writeTar(t, old, map[string]string{"watch.json": "{}", "tmdb/matches.json": "{}"})
	files, _, err := readBackup(old)
	if err != nil || files["data/watch.json"] == nil || files["data/tmdb/matches.json"] == nil {
		t.Fatalf("old archive: %v %v", err, files)
	}
	// The first Go backups used config/.
	first := filepath.Join(dir, "first.tgz")
	m, _ = json.Marshal(backupManifest{Schema: 1})
	writeTar(t, first, map[string]string{"manifest.json": string(m), "config/devices.json": "{}"})
	if files, _, err := readBackup(first); err != nil || files["conf/devices.json"] == nil {
		t.Fatalf("config/ archive: %v %v", err, files)
	}
}

func TestPruneBackups(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 4, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, "state-2026-01-0"+string(rune('1'+i))+".tgz")
		os.WriteFile(p, nil, 0o600)
		ts := base.AddDate(0, 0, i)
		os.Chtimes(p, ts, ts)
	}
	os.WriteFile(filepath.Join(dir, "upgrade-0to1-1.tgz"), nil, 0o600)
	if n := pruneBackups(dir, "state-", 3); n != 2 {
		t.Fatalf("pruned %d", n)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "state-*.tgz"))
	if len(left) != 3 || filepath.Base(left[0]) != "state-2026-01-03.tgz" {
		t.Fatalf("after pruning: %v", left)
	}
	if _, err := os.Stat(filepath.Join(dir, "upgrade-0to1-1.tgz")); err != nil {
		t.Fatal("pruning state- removed an upgrade backup")
	}
}

func TestMkdirWithinTimesOut(t *testing.T) {
	if err := mkdirWithin(filepath.Join(t.TempDir(), "a", "b"), time.Second); err != nil {
		t.Fatal(err)
	}
}
