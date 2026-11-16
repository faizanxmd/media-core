package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetEnvLineKeepsTheRest(t *testing.T) {
	p := filepath.Join(t.TempDir(), "engine.env")
	os.WriteFile(p, []byte("# mine\nMEDIA_CORE_NAME=old\nMEDIA_CORE_OLD=1\n"), 0o600)
	if err := setEnvLine(p, "MEDIA_CORE_NAME", "Den"); err != nil {
		t.Fatal(err)
	}
	setEnvLine(p, "MEDIA_CORE_LIBRARY", "/srv/media")
	setEnvLine(p, "MEDIA_CORE_OLD", "")
	b, _ := os.ReadFile(p)
	if got := string(b); got != "# mine\nMEDIA_CORE_NAME=Den\nMEDIA_CORE_LIBRARY=/srv/media\n" {
		t.Fatalf("engine.env =\n%s", got)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestSetupConfig(t *testing.T) {
	oldConf, oldMedia, oldCheck, oldRestart := ConfDir, Media, checkTMDBKey, restartSelf
	defer func() { ConfDir, Media, checkTMDBKey, restartSelf = oldConf, oldMedia, oldCheck, oldRestart }()
	ConfDir, Media = t.TempDir(), t.TempDir()
	restarts := 0
	restartSelf = func(func()) { restarts++ }
	checkTMDBKey = func(k string) error {
		if k != "good" {
			return errors.New("TMDB doesn't accept that key")
		}
		return nil
	}
	busy := ""
	s := &setupAPI{busy: func() string { return busy }, flush: func() {}}
	mux := http.NewServeMux()
	s.mount(mux)
	post := func(body any) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, jsonReq("POST", "/api/setup/config", body, ""))
		return rec
	}
	if rec := post(map[string]any{"tmdb_key": "bad"}); rec.Code != 400 || !strings.Contains(rec.Body.String(), "doesn't accept") {
		t.Fatalf("bad key: %d %s", rec.Code, rec.Body)
	}
	if rec := post(map[string]any{"tmdb_key": "good"}); rec.Code != 200 {
		t.Fatalf("good key: %d %s", rec.Code, rec.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(ConfDir, "tmdb.key")); strings.TrimSpace(string(b)) != "good" {
		t.Fatal("key not saved")
	}
	newLib := filepath.Join(t.TempDir(), "lib")
	if rec := post(map[string]any{"library": newLib}); rec.Code != 400 {
		t.Fatalf("missing folder accepted: %d", rec.Code)
	}
	busy = "the downloads finish"
	if rec := post(map[string]any{"library": newLib, "make_folders": true}); rec.Code != 409 {
		t.Fatalf("move while busy: %d", rec.Code)
	}
	busy = ""
	if rec := post(map[string]any{"library": newLib, "make_folders": true}); rec.Code != 200 {
		t.Fatalf("move: %d %s", rec.Code, rec.Body)
	}
	for _, f := range libraryFolders {
		if _, err := os.Stat(filepath.Join(newLib, f)); err != nil {
			t.Fatalf("%s not made", f)
		}
	}
	env, _ := os.ReadFile(filepath.Join(ConfDir, "engine.env"))
	if !strings.Contains(string(env), "MEDIA_CORE_LIBRARY="+newLib+"\n") {
		t.Fatalf("engine.env = %s", env)
	}
	if rec := post(map[string]any{"library": "relative/path"}); rec.Code != 400 {
		t.Fatalf("relative path accepted: %d", rec.Code)
	}
	if restarts != 2 {
		t.Fatalf("restarts = %d, want 2", restarts)
	}
	s.temp = true
	if rec := post(map[string]any{"library": t.TempDir()}); rec.Code != 400 {
		t.Fatalf("temporary server moved its library: %d", rec.Code)
	}
}
