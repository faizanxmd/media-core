package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var realServerBusy = serverBusy

const oldBinary = "#!/bin/sh\n[ \"$1\" = version ] && echo media-core-engine old\nexit 0\n"

// fakeBinary: a script that says it is version v and accepts install.
func fakeBinary(v string) string {
	return "#!/bin/sh\n[ \"$1\" = version ] && echo media-core-engine " + v + "\nexit 0\n"
}

func engineAsset() string { return "media-core-engine-" + runtime.GOOS + "-" + runtime.GOARCH }

// upgradeFixture: an installed old binary, a fake system, and a release v2.0.0 holding newBin.
func upgradeFixture(t *testing.T, newBin, wrongSum string) (bin string, sys *fakeSystem, host *releaseHost) {
	fakeHome(t)
	bin = filepath.Join(t.TempDir(), "media-core-engine")
	writeFile(t, bin, oldBinary, 0o755)
	sys = stubSystem(t, bin)
	host = fakeReleaseHost(t, "v2.0.0", map[string]string{engineAsset(): newBin}, "SHA256SUMS", wrongSum)
	return bin, sys, host
}

func TestUpgradeRefusesABadChecksum(t *testing.T) {
	bin, _, _ := upgradeFixture(t, fakeBinary("2.0.0"), engineAsset())
	_, err := runUpgrade(upgradeOpts{}, say(t))
	if err == nil || err.code != exitFailed || !strings.Contains(err.msg, "doesn't match") {
		t.Fatalf("got %v", err)
	}
	if readFile(t, bin) != oldBinary {
		t.Fatal("the binary was changed")
	}
	if fileExists(filepath.Join(filepath.Dir(bin), ".media-core-engine.new")) {
		t.Fatal("the download was left behind")
	}
}

func TestUpgradeRefusesTheWrongVersion(t *testing.T) {
	bin, _, _ := upgradeFixture(t, fakeBinary("1.9.9"), "")
	_, err := runUpgrade(upgradeOpts{}, say(t))
	if err == nil || err.code != exitFailed || !strings.Contains(err.msg, "1.9.9") {
		t.Fatalf("got %v", err)
	}
	if readFile(t, bin) != oldBinary || fileExists(bin+".prev") {
		t.Fatal("something was swapped")
	}
}

func TestUpgradeWaitsWhileBusy(t *testing.T) {
	_, sys, host := upgradeFixture(t, fakeBinary("2.0.0"), "")
	sys.busy = "the downloads finish"
	_, err := runUpgrade(upgradeOpts{}, say(t))
	if err == nil || err.code != exitBusy {
		t.Fatalf("got %v", err)
	}
	if host.asked(engineAsset()) || host.asked("SHA256SUMS") {
		t.Fatal("it downloaded while busy")
	}
}

func TestUpgradeRollsBackWhenTheNewOneDoesntAnswer(t *testing.T) {
	bin, _, _ := upgradeFixture(t, fakeBinary("2.0.0"), "")
	helloServer(t, func() string { return version }) // the old version keeps answering; 2.0.0 never does
	res, err := runUpgrade(upgradeOpts{}, say(t))
	if err == nil || err.code != exitPartial {
		t.Fatalf("got %v", err)
	}
	if !res.RolledBack || readFile(t, bin) != oldBinary {
		t.Fatalf("not rolled back: %+v\n%s", res, readFile(t, bin))
	}
}

func TestUpgradeSucceeds(t *testing.T) {
	bin, _, _ := upgradeFixture(t, fakeBinary("2.0.0"), "")
	helloServer(t, func() string { return "2.0.0" })
	res, err := runUpgrade(upgradeOpts{}, say(t))
	if err != nil || !res.OK || res.RolledBack {
		t.Fatalf("got %v %+v", err, res)
	}
	if readFile(t, bin) != fakeBinary("2.0.0") || readFile(t, bin+".prev") != oldBinary {
		t.Fatal("binary and .prev aren't new and old")
	}
}

func TestUpgradeAppsWritesEachAPKBeforeItsJSON(t *testing.T) {
	fakeHome(t)
	stubSystem(t, filepath.Join(t.TempDir(), "x"))
	assets := map[string]string{
		"media-core.apk": "tv apk", "media-core.json": `{"version_code":1,"version_name":"` + version + `"}`,
		"media-core-phone.apk": "phone apk", "media-core-phone.json": `{"version_code":1,"version_name":"` + version + `"}`,
	}
	fakeReleaseHost(t, "v"+version, assets, "SHA256SUMS", "")
	var order []string
	old := renameFile
	renameFile = func(a, b string) error { order = append(order, filepath.Base(b)); return old(a, b) }
	t.Cleanup(func() { renameFile = old })
	if _, err := runUpgrade(upgradeOpts{version: version, apps: true}, say(t)); err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	for _, app := range []string{"media-core", "media-core-phone"} {
		if pos[app+".apk"] > pos[app+".json"] || len(order) != 4 {
			t.Fatalf("order %v", order)
		}
	}
	if readFile(t, filepath.Join(DataDir, "app", "media-core.apk")) != "tv apk" {
		t.Fatal("the APK wasn't written")
	}
}

func TestUpgradeAppsRefusesABadChecksum(t *testing.T) {
	fakeHome(t)
	stubSystem(t, filepath.Join(t.TempDir(), "x"))
	assets := map[string]string{"media-core.apk": "a", "media-core.json": "{}", "media-core-phone.apk": "b", "media-core-phone.json": "{}"}
	fakeReleaseHost(t, "v"+version, assets, "APPS.SHA256SUMS", "media-core-phone.apk")
	if err := fetchApps("v" + version); err == nil {
		t.Fatal("a bad APK was accepted")
	}
	if fileExists(filepath.Join(DataDir, "app", "media-core.json")) {
		t.Fatal("files were written before every one was checked")
	}
}

// An engine from before GET /api/busy: the jobs and the system page decide.
func TestBusyFallsBackForOlderServers(t *testing.T) {
	fakeHome(t)
	jobs, playing := `{"jobs":[{"state":"done"}]}`, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/jobs":
			w.Write([]byte(jobs))
		case "/api/system":
			json.NewEncoder(w).Encode(map[string]int{"playing": playing})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	t.Setenv("MEDIA_CORE_LISTEN", ":"+port)
	if why, up := realServerBusy(); why != "" || !up {
		t.Fatalf("idle: %q %v", why, up)
	}
	playing = 1
	if why, _ := realServerBusy(); why == "" {
		t.Fatal("playing wasn't seen")
	}
	playing, jobs = 0, `{"jobs":[{"state":"downloading"}]}`
	if why, _ := realServerBusy(); !strings.Contains(why, "downloads") {
		t.Fatalf("downloading: %q", why)
	}
	os.Remove(cliTokenPath())
}
