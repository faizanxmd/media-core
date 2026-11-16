package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files in testdata")

func goldenUnits(d unitData) string {
	units := renderUnits(d)
	var b strings.Builder
	for _, name := range unitNames {
		if text, ok := units[name]; ok {
			b.WriteString("### " + name + "\n" + text + "\n")
		}
	}
	return b.String()
}

func TestUnitsGolden(t *testing.T) {
	got := goldenUnits(unitData{Bin: "/srv/u/.local/bin/media-core-engine", Aria2c: "/usr/bin/aria2c", ConfDir: "/srv/u/.config/media-core",
		DataDir: "/srv/u/.local/share/media-core", Path: "/srv/u/.local/bin:/usr/local/bin:/usr/bin:/bin", DefaultDirs: true})
	golden := filepath.Join("testdata", "units.golden")
	if *updateGolden {
		os.WriteFile(golden, []byte(got), 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("the units changed; if that's meant, run: go test -run TestUnitsGolden -update\n%s", got)
	}
	for _, name := range unitNames {
		if !strings.HasPrefix(renderUnits(unitData{Aria2c: "/a"})[name], unitHeader) {
			t.Fatalf("%s doesn't start with the header line", name)
		}
	}
}

func TestUnitsOtherFoldersAndSpaces(t *testing.T) {
	u := renderUnits(unitData{Bin: "/srv/my media/media-core-engine", Aria2c: "/opt/bin/aria2c", ConfDir: "/srv/my media/conf",
		DataDir: "/srv/data", Path: "/srv/my media:/usr/bin"})
	for _, want := range []string{
		`ExecStart="/srv/my media/media-core-engine" serve`,
		`Environment="MEDIA_CORE_CONFIG_DIR=/srv/my media/conf"`,
		`Environment=MEDIA_CORE_DATA_DIR=/srv/data`,
		`Environment="PATH=/srv/my media:/usr/bin"`,
	} {
		if !strings.Contains(u[engineUnit], want) {
			t.Fatalf("engine unit lacks %s:\n%s", want, u[engineUnit])
		}
	}
	if !strings.Contains(u[aria2Unit], `ExecStart=/opt/bin/aria2c "--conf-path=/srv/my media/conf/aria2.conf"`) {
		t.Fatalf("aria2 unit:\n%s", u[aria2Unit])
	}
	if !strings.Contains(u[backupUnit], "MEDIA_CORE_DATA_DIR=/srv/data") {
		t.Fatalf("the backup unit doesn't know the data folder:\n%s", u[backupUnit])
	}
	for name, text := range u {
		if strings.Contains(text, "{{") || strings.Contains(text, "<no value>") {
			t.Fatalf("%s: unfilled template:\n%s", name, text)
		}
	}
}

func TestNoAria2UnitWithoutAria2c(t *testing.T) {
	dir := t.TempDir()
	changed, err := writeUnitsFrom(dir, unitData{Bin: "/b/media-core-engine", ConfDir: "/c", DataDir: "/d", Path: "/b", DefaultDirs: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 3 || contains(changed, aria2Unit) {
		t.Fatalf("changed = %v", changed)
	}
	if fileExists(filepath.Join(dir, aria2Unit)) {
		t.Fatal("an aria2 unit was written with no aria2c")
	}
	if strings.Contains(readFile(t, filepath.Join(dir, engineUnit)), "media-core-aria2") {
		t.Fatal("the engine unit wants an aria2 unit that isn't there")
	}
}

func TestWriteUnitsOnlyWhenChanged(t *testing.T) {
	dir := t.TempDir()
	d := unitData{Bin: "/b/media-core-engine", Aria2c: "/usr/bin/aria2c", ConfDir: "/c", DataDir: "/d", Path: "/b", DefaultDirs: true}
	if changed, err := writeUnitsFrom(dir, d); err != nil || len(changed) != 4 {
		t.Fatalf("first write: %v %v", changed, err)
	}
	if changed, err := writeUnitsFrom(dir, d); err != nil || len(changed) != 0 {
		t.Fatalf("second write changed %v (%v)", changed, err)
	}
	d.Bin = "/other/media-core-engine"
	if changed, _ := writeUnitsFrom(dir, d); len(changed) != 2 || !contains(changed, engineUnit) || !contains(changed, backupUnit) {
		t.Fatalf("after moving the binary: %v", changed)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 4 {
		t.Fatalf("temp files left behind: %v", ents)
	}
}

func TestUnitsPassSystemdVerify(t *testing.T) {
	analyze, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("no systemd-analyze here")
	}
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true here")
	}
	dir := t.TempDir()
	if _, err := writeUnitsFrom(dir, unitData{Bin: truePath, Aria2c: truePath, ConfDir: "/srv/u/conf", DataDir: "/srv/u/data", Path: "/usr/bin:/bin"}); err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, n := range unitNames {
		files = append(files, filepath.Join(dir, n))
	}
	cmd := exec.Command(analyze, append([]string{"--user", "verify"}, files...)...)
	cmd.Env = append(os.Environ(), "SYSTEMD_UNIT_PATH="+dir+":")
	out, err := cmd.CombinedOutput()
	// only what it says about our own units counts: the host's other units are not this test's business
	var ours []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "media-core-") {
			ours = append(ours, l)
		}
	}
	if len(ours) > 0 {
		t.Fatalf("systemd-analyze verify: %v\n%s", err, strings.Join(ours, "\n"))
	}
}
