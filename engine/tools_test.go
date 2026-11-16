package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAria2ConfText(t *testing.T) {
	c := aria2ConfText("s3cret", "/data", "none", 6800)
	for _, want := range []string{"rpc-secret=s3cret", "input-file=/data/aria2.session", "file-allocation=none", "rpc-listen-all=false", "rpc-listen-port=6800"} {
		if !strings.Contains(c, want+"\n") {
			t.Errorf("conf missing %q", want)
		}
	}
}

func TestEnsureAria2ConfKeepsExisting(t *testing.T) {
	oldConf, oldData := Aria2Conf, DataDir
	defer func() { Aria2Conf, DataDir = oldConf, oldData }()
	dir := t.TempDir()
	Aria2Conf, DataDir = filepath.Join(dir, "conf", "aria2.conf"), filepath.Join(dir, "data")

	if err := ensureAria2Conf(6800); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Aria2Conf)
	if !strings.Contains(string(b), "rpc-secret=") || strings.Contains(string(b), "rpc-secret=\n") {
		t.Fatalf("no secret written:\n%s", b)
	}
	if fi, _ := os.Stat(Aria2Conf); fi.Mode().Perm() != 0o600 {
		t.Fatalf("conf mode = %v, want 0600", fi.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(DataDir, "aria2.session")); err != nil {
		t.Fatal("no session file")
	}
	os.WriteFile(Aria2Conf, []byte("mine"), 0o600)
	ensureAria2Conf(6800)
	if b, _ := os.ReadFile(Aria2Conf); string(b) != "mine" {
		t.Fatal("an existing aria2.conf was overwritten")
	}
}

func TestMissingToolSaysHowToFix(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	oldDovi := DoviTool
	DoviTool = "dovi_tool"
	defer func() { DoviTool = oldDovi }()
	err := needTool("Unpacking", "7z")
	if err == nil {
		t.Fatal("7z found on an empty PATH")
	}
	if msg := err.Error(); !strings.HasPrefix(msg, "Unpacking needs 7z. On the server: ") {
		t.Fatalf("message = %q", msg)
	}
	for _, s := range toolStates() {
		if s.Found || s.Install == "" {
			t.Errorf("%s: found=%v install=%q on an empty PATH", s.ID, s.Found, s.Install)
		}
	}
}

func TestLowPrioArgv(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no ionice, no nice: the tool runs as it is
	if prog, args := lowPrioArgv(true, "19", "ffmpeg", "-i", "x"); prog != "ffmpeg" || strings.Join(args, " ") != "-i x" {
		t.Fatalf("got %s %v", prog, args)
	}
}
