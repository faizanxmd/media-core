package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func setupFixture(t *testing.T) (home string, sys *fakeSystem) {
	home = fakeHome(t)
	sys = stubSystem(t, filepath.Join(home, ".local/bin/media-core-engine"))
	old := checkTMDBKey
	t.Cleanup(func() { checkTMDBKey = old })
	checkTMDBKey = func(k string) error {
		if k != "good" && k != "good2" {
			return errors.New("TMDB doesn't accept that key")
		}
		return nil
	}
	return home, sys
}

func TestSetupFlagsWriteTheRightLines(t *testing.T) {
	home, sys := setupFixture(t)
	sys.answer["is-active "+engineUnit] = "active"
	lib := filepath.Join(home, "films")
	res, err := runSetup(installOpts{library: lib, makeFolders: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Changed, ",") != "library" || !res.Restarted || res.Library != lib {
		t.Fatalf("library: %+v", res)
	}
	if len(missingFolders(lib)) != 0 {
		t.Fatal("--make-folders didn't make the section folders")
	}
	setPaths(ConfDir, DataDir, lib) // what a restarted server would read
	res, err = runSetup(installOpts{name: "Living Room"})
	if err != nil || strings.Join(res.Changed, ",") != "name" || res.Name != "Living Room" {
		t.Fatalf("name: %+v %v", res, err)
	}
	keyFile := filepath.Join(home, "key.txt")
	writeFile(t, keyFile, "good\n", 0o600)
	res, err = runSetup(installOpts{tmdbKeyFile: keyFile})
	if err != nil || strings.Join(res.Changed, ",") != "tmdb" || !res.TMDB {
		t.Fatalf("key file: %+v %v", res, err)
	}
	if got := readFile(t, filepath.Join(ConfDir, "engine.env")); got != "MEDIA_CORE_LIBRARY="+lib+"\nMEDIA_CORE_NAME=Living Room\n" {
		t.Fatalf("engine.env:\n%s", got)
	}
	if readFile(t, filepath.Join(ConfDir, "tmdb.key")) != "good\n" {
		t.Fatal("tmdb.key")
	}

	// the same key again changes nothing and restarts nothing; the environment gives a key too
	sys.calls = nil
	res, err = runSetup(installOpts{tmdbKey: "good"})
	if err != nil || len(res.Changed) != 0 || res.Restarted || sys.called("restart") {
		t.Fatalf("same key: %+v %v %v", res, err, sys.calls)
	}
	t.Setenv("MEDIA_CORE_TMDB_KEY", "good2")
	if res, err = runSetup(installOpts{}); err != nil || strings.Join(res.Changed, ",") != "tmdb" {
		t.Fatalf("key from the environment: %+v %v", res, err)
	}
}

func TestSetupRefusals(t *testing.T) {
	home, sys := setupFixture(t)
	code := func(o installOpts) int {
		t.Helper()
		_, err := runSetup(o)
		if err == nil {
			return 0
		}
		return err.code
	}
	if c := code(installOpts{library: "relative/path"}); c != exitUsage {
		t.Fatalf("a relative path: exit %d", c)
	}
	if c := code(installOpts{library: filepath.Join(home, "missing")}); c != exitFailed {
		t.Fatalf("a missing folder without --make-folders: exit %d", c)
	}
	if c := code(installOpts{}); c != exitUsage {
		t.Fatalf("no flags and no terminal: exit %d", c)
	}
	writeFile(t, filepath.Join(ConfDir, "tmdb.key"), "good\n", 0o600)
	if c := code(installOpts{tmdbKey: "bad"}); c != exitFailed {
		t.Fatalf("a bad key: exit %d", c)
	}
	if readFile(t, filepath.Join(ConfDir, "tmdb.key")) != "good\n" {
		t.Fatal("a bad key replaced the saved one")
	}
	// a bad key stops the whole change: the library isn't half-saved
	lib := filepath.Join(home, "films")
	if c := code(installOpts{library: lib, makeFolders: true, tmdbKey: "bad"}); c != exitFailed {
		t.Fatalf("bad key with a library: exit %d", c)
	}
	if fileExists(filepath.Join(ConfDir, "engine.env")) {
		t.Fatal("the library was saved although the key was refused")
	}
	sys.busy = "nothing is playing"
	if c := code(installOpts{library: lib, yes: true}); c != exitBusy {
		t.Fatalf("busy: exit %d", c)
	}
	if fileExists(filepath.Join(ConfDir, "engine.env")) || sys.called("restart") {
		t.Fatal("busy still changed something")
	}
}

// The site's Settings page and the command go through one function, so they agree.
func TestSetupCommandMatchesTheSite(t *testing.T) {
	home, _ := setupFixture(t)
	oldRestart := restartSelf
	defer func() { restartSelf = oldRestart }()
	restartSelf = func(func()) {}
	mux := http.NewServeMux()
	(&setupAPI{busy: func() string { return "" }, flush: func() {}}).mount(mux)
	lib, missing := filepath.Join(home, "films"), filepath.Join(home, "missing")
	for _, f := range libraryFolders {
		writeFile(t, filepath.Join(lib, f, ".keep"), "", 0o644)
	}
	for _, tc := range []struct {
		name string
		o    installOpts
		body map[string]any
	}{
		{"relative", installOpts{library: "x/y"}, map[string]any{"library": "x/y"}},
		{"missing", installOpts{library: missing}, map[string]any{"library": missing}},
		{"bad key", installOpts{tmdbKey: "bad"}, map[string]any{"tmdb_key": "bad"}},
		{"all three", installOpts{library: lib, name: "Den", tmdbKey: "good"}, map[string]any{"library": lib, "name": "Den", "tmdb_key": "good"}},
	} {
		clear := func() {
			for _, f := range []string{"engine.env", "tmdb.key"} {
				writeFile(t, filepath.Join(ConfDir, f), "", 0o600)
			}
		}
		clear()
		_, cerr := runSetup(tc.o)
		fromCmd := readFile(t, filepath.Join(ConfDir, "engine.env")) + "|" + readFile(t, filepath.Join(ConfDir, "tmdb.key"))
		clear()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, jsonReq("POST", "/api/setup/config", tc.body, ""))
		fromSite := readFile(t, filepath.Join(ConfDir, "engine.env")) + "|" + readFile(t, filepath.Join(ConfDir, "tmdb.key"))
		if (cerr == nil) != (rec.Code == 200) || fromCmd != fromSite {
			t.Fatalf("%s: the command gave %v and %q; the site gave %d and %q", tc.name, cerr, fromCmd, rec.Code, fromSite)
		}
		if cerr != nil && !strings.Contains(rec.Body.String(), strings.Split(cerr.msg, "\n")[0]) {
			t.Fatalf("%s: different reasons: %q and %s", tc.name, cerr.msg, rec.Body)
		}
	}
}

func TestBusyRoute(t *testing.T) {
	why := "the downloads finish"
	mux := http.NewServeMux()
	(&setupAPI{busy: func() string { return why }}).mount(mux)
	get := func() string {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/busy", nil))
		return strings.TrimSpace(rec.Body.String())
	}
	if got := get(); got != `{"busy":true,"why":"the downloads finish"}` {
		t.Fatalf("busy: %s", got)
	}
	why = ""
	if got := get(); got != `{"busy":false,"why":""}` {
		t.Fatalf("idle: %s", got)
	}
	if playerRoute("/api/busy") {
		t.Fatal("/api/busy must need an admin")
	}
}
