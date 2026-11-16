package main

import (
	"os"
	"path/filepath"
	"testing"
)

func uninstallFixture(t *testing.T) (*fakeSystem, string) {
	home := fakeHome(t)
	sys := stubSystem(t, filepath.Join(t.TempDir(), "other-name"))
	writeFile(t, binPath(), "bin", 0o755)
	writeFile(t, filepath.Join(unitDir(), engineUnit), "unit", 0o644)
	writeFile(t, filepath.Join(ConfDir, "devices.json"), "{}", 0o600)
	writeFile(t, filepath.Join(DataDir, "watch.json"), "{}", 0o600)
	lib := filepath.Join(home, "films")
	writeFile(t, filepath.Join(lib, "Movies", "A (2020)", "A (2020).mkv"), "film", 0o644)
	Media = lib
	return sys, lib
}

func TestUninstallAsksFirst(t *testing.T) {
	uninstallFixture(t)
	if _, err := runUninstall(uninstallOpts{}, say(t), func(string) bool { return true }); err == nil || err.code != exitUsage {
		t.Fatalf("no terminal, no --yes: %v", err)
	}
	stdinTTY = func() bool { return true }
	if _, err := runUninstall(uninstallOpts{}, say(t), func(string) bool { return false }); err == nil {
		t.Fatal("a no went on")
	}
	if !fileExists(binPath()) {
		t.Fatal("removed after a no")
	}
}

func TestUninstallWaitsWhileBusy(t *testing.T) {
	sys, _ := uninstallFixture(t)
	sys.busy = "nothing is playing"
	if _, err := runUninstall(uninstallOpts{yes: true}, say(t), nil); err == nil || err.code != exitBusy {
		t.Fatalf("busy: %v", err)
	}
	if !fileExists(binPath()) || sys.called("disable") {
		t.Fatal("something was removed while busy")
	}
	if _, err := runUninstall(uninstallOpts{yes: true, force: true}, say(t), nil); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallKeepsDataAndLibrary(t *testing.T) {
	_, lib := uninstallFixture(t)
	res, err := runUninstall(uninstallOpts{yes: true}, say(t), nil)
	if err != nil || !res.OK {
		t.Fatal(err)
	}
	if fileExists(binPath()) || fileExists(filepath.Join(unitDir(), engineUnit)) {
		t.Fatal("binary or unit left")
	}
	if !fileExists(filepath.Join(ConfDir, "devices.json")) || !fileExists(filepath.Join(DataDir, "watch.json")) {
		t.Fatal("settings or progress removed without --purge")
	}
	if !fileExists(filepath.Join(lib, "Movies", "A (2020)", "A (2020).mkv")) {
		t.Fatal("the library was touched")
	}
}

func TestUninstallPurge(t *testing.T) {
	_, lib := uninstallFixture(t)
	if _, err := runUninstall(uninstallOpts{yes: true, purge: true}, say(t), nil); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{ConfDir, DataDir} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("%s is still there", d)
		}
	}
	if !fileExists(filepath.Join(lib, "Movies", "A (2020)", "A (2020).mkv")) {
		t.Fatal("--purge touched the library")
	}
}

func TestUninstallPurgeRefusesALibraryInsideTheData(t *testing.T) {
	uninstallFixture(t)
	Media = filepath.Join(DataDir, "library")
	writeFile(t, filepath.Join(Media, "x"), "film", 0o644)
	if _, err := runUninstall(uninstallOpts{yes: true, purge: true}, say(t), nil); err == nil {
		t.Fatal("purged a library inside the data folder")
	}
	if !fileExists(filepath.Join(Media, "x")) {
		t.Fatal("the library was deleted")
	}
}
