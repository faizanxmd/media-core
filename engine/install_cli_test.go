package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installFixture(t *testing.T) (home string, sys *fakeSystem) {
	home = fakeHome(t)
	exe := filepath.Join(t.TempDir(), "media-core-engine")
	writeFile(t, exe, "#!/bin/sh\necho the engine\n", 0o755)
	sys = stubSystem(t, exe)
	helloServer(t, func() string { return version })
	return home, sys
}

func TestInstallFirstRunAndAgain(t *testing.T) {
	home, sys := installFixture(t)
	sys.answer["loginctl show-user "+userName()+" -p Linger --value"] = "no"
	sys.fail["loginctl enable-linger"] = true
	lib := filepath.Join(home, "films")
	res, err := runInstall(installOpts{library: lib, makeFolders: true, name: "Den", yes: true, noApps: true}, say(t))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, ".local/bin/media-core-engine")
	if res.Binary != bin || readFile(t, bin) != "#!/bin/sh\necho the engine\n" {
		t.Fatalf("binary: %s", res.Binary)
	}
	if res.Service != "running" || res.Claimed || res.SetupCode == "" || res.Library != lib || res.LibraryProblem != "" || !res.OK {
		t.Fatalf("result: %+v", res)
	}
	if !strings.HasSuffix(res.SetupURL, "/#/setup") || len(res.URLs) == 0 {
		t.Fatalf("urls: %+v", res)
	}
	if !contains(res.NeedsSudo, "sudo loginctl enable-linger "+userName()) {
		t.Fatalf("needs_sudo lacks the linger line: %v", res.NeedsSudo)
	}
	env := readFile(t, filepath.Join(ConfDir, "engine.env"))
	if !strings.Contains(env, "MEDIA_CORE_LIBRARY="+lib+"\n") || !strings.Contains(env, "MEDIA_CORE_NAME=Den\n") {
		t.Fatalf("engine.env:\n%s", env)
	}
	for p, mode := range map[string]os.FileMode{ConfDir: 0o700, Aria2Conf: 0o600, filepath.Join(ConfDir, "engine.env"): 0o600} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != mode {
			t.Fatalf("%s: %v, want mode %v", p, err, mode)
		}
	}
	if !strings.Contains(readFile(t, filepath.Join(unitDir(), engineUnit)), "ExecStart="+bin+" serve") {
		t.Fatal("the engine unit doesn't run the installed binary")
	}
	for _, want := range []string{"daemon-reload", "enable " + engineUnit, "enable --now " + backupTimer, "restart " + engineUnit} {
		if !sys.called(want) {
			t.Fatalf("systemctl %s wasn't run: %v", want, sys.calls)
		}
	}

	// again: nothing is overwritten, and a server already on this version with unchanged units isn't restarted
	conf, data := treeHash(t, ConfDir), readFile(t, Aria2Conf)
	os.Remove(filepath.Join(ConfDir, "setup.code"))
	conf = treeHash(t, ConfDir)
	sys.calls = nil
	sys.answer["is-active "+engineUnit] = "active"
	sys.answer["loginctl show-user "+userName()+" -p Linger --value"] = "yes"
	res, err = runInstall(installOpts{yes: true, noApps: true}, say(t))
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(ConfDir, "setup.code"))
	if treeHash(t, ConfDir) != conf || readFile(t, Aria2Conf) != data {
		t.Fatal("a second install changed the settings")
	}
	if sys.called("restart") {
		t.Fatalf("a second install restarted a server with nothing new to run: %v", sys.calls)
	}
	if res.Library != lib || len(res.NeedsSudo) != len(mustSudo()) {
		t.Fatalf("second result: %+v", res)
	}
}

func mustSudo() []string { s, _ := missingTools(); return s }

func TestInstallNeverRestartsWhileBusy(t *testing.T) {
	_, sys := installFixture(t)
	sys.busy = "the downloads finish"
	sys.answer["is-active "+engineUnit] = "active"
	old := version
	version = "9.9.9"              // the running server (the hello stub) is on another version
	helloServer(t, func() string { // the old server, until the (fake) restart brings this version up
		if sys.called("restart " + engineUnit) {
			return version
		}
		return old
	})
	defer func() { version = old }()
	res, err := runInstall(installOpts{yes: true, noApps: true}, say(t))
	if err != nil {
		t.Fatal(err)
	}
	if sys.called("restart") {
		t.Fatalf("restarted while busy: %v", sys.calls)
	}
	if res.Service != "running" || len(res.Warnings) == 0 || !strings.Contains(strings.Join(res.Warnings, " "), "the downloads finish") {
		t.Fatalf("result: %+v", res)
	}
	// --force is how upgrade says the check was already made
	sys.calls = nil
	sys.busy = "the downloads finish"
	if _, err := runInstall(installOpts{yes: true, noApps: true, force: true}, say(t)); err != nil {
		t.Fatal(err)
	}
	if !sys.called("restart " + engineUnit) {
		t.Fatalf("--force didn't restart: %v", sys.calls)
	}
}

func TestInstallWithoutAUserSession(t *testing.T) {
	home, sys := installFixture(t)
	hasSystemd = func() bool { return false }
	if _, err := runInstall(installOpts{yes: true, noApps: true}, say(t)); err == nil || err.code != exitUnsupported {
		t.Fatalf("no session: %v", err)
	}
	if fileExists(filepath.Join(home, ".local/bin/media-core-engine")) {
		t.Fatal("something was installed before the refusal")
	}
	res, err := runInstall(installOpts{yes: true, noApps: true, noService: true}, say(t))
	if err != nil {
		t.Fatal(err)
	}
	if res.Service != "not-installed" || res.SetupCode != "" || len(sys.calls) != 0 || fileExists(filepath.Join(unitDir(), engineUnit)) {
		t.Fatalf("--no-service: %+v, calls %v", res, sys.calls)
	}
	if res.LibraryProblem == "" {
		t.Fatal("a missing default library should be reported, not made")
	}
	if !fileExists(Aria2Conf) {
		t.Fatal("no aria2.conf")
	}
}

func TestInstallServerThatNeverAnswers(t *testing.T) {
	_, _ = installFixture(t)
	noServer(t)
	helloWait = 300_000_000
	res, err := runInstall(installOpts{yes: true, noApps: true}, say(t))
	if err == nil || err.code != exitFailed || res == nil || res.Service != "failed" || !strings.Contains(err.msg, "journal of") {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestInstallPicksAFreeAria2Port(t *testing.T) {
	installFixture(t)
	portTaken = func(p int) bool { return p == 6800 }
	if _, err := runInstall(installOpts{yes: true, noApps: true, noService: true}, say(t)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFile(t, Aria2Conf), "rpc-listen-port=6800\n") {
		t.Fatal("aria2 was given a port something else is using")
	}
}

func TestInstallBadLibrary(t *testing.T) {
	home, _ := installFixture(t)
	if _, err := runInstall(installOpts{library: "films", yes: true, noApps: true}, say(t)); err == nil || err.code != exitUsage {
		t.Fatalf("relative library: %v", err)
	}
	if _, err := runInstall(installOpts{library: filepath.Join(home, "nope"), yes: true, noApps: true}, say(t)); err == nil || err.code != exitFailed {
		t.Fatalf("missing library: %v", err)
	}
}
