package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeRelease(t *testing.T, files map[string]string, tamper string) {
	t.Helper()
	var sums strings.Builder
	for name, body := range files {
		h := sha256.Sum256([]byte(body))
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(h[:]), name)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "APPS.SHA256SUMS" {
			w.Write([]byte(sums.String()))
			return
		}
		body, ok := files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if name == tamper {
			body += "x"
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MEDIA_CORE_BASE_URL", srv.URL)
}

func TestFetchApps(t *testing.T) {
	files := map[string]string{
		"media-core.apk": "tv-apk", "media-core.json": `{"version_code":2,"version_name":"b","size":6}`,
		"media-core-phone.apk": "phone-apk", "media-core-phone.json": `{"version_code":3,"version_name":"c","size":9}`,
	}
	dir := filepath.Join(DataDir, "app")
	t.Cleanup(func() { os.RemoveAll(dir) })

	fakeRelease(t, files, "media-core-phone.apk")
	if err := fetchApps("v1.0.0"); err == nil {
		t.Fatal("a tampered APK was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "media-core.json")); err == nil {
		t.Fatal("files were written although one didn't match")
	}

	fakeRelease(t, files, "")
	if err := fetchApps("v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if a := readAppInfo("media-core"); a == nil || a.VersionCode != 2 {
		t.Fatalf("TV app info: %+v", a)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "media-core-phone.apk")); string(b) != "phone-apk" {
		t.Fatalf("phone APK: %q", b)
	}
}

func TestSumFor(t *testing.T) {
	sums := "abc  media-core-engine-linux-amd64\ndef *install.sh\n"
	if sumFor(sums, "install.sh") != "def" || sumFor(sums, "media-core-engine-linux-amd64") != "abc" || sumFor(sums, "x") != "" {
		t.Fatal("sumFor")
	}
}

func TestUnitsUseRealPaths(t *testing.T) {
	v := unitVars{Bin: "/srv/u/.local/bin/media-core-engine", Aria2c: "/opt/bin/aria2c", Conf: "/srv/u/.config/media-core", Data: "/d", Path: "/p"}
	for _, n := range unitNames {
		text := render(unitTemplates[n], v)
		if strings.Contains(text, "{{") || strings.Contains(text, "<no value>") {
			t.Fatalf("%s: unfilled template:\n%s", n, text)
		}
	}
	if !strings.Contains(render(unitTemplates["media-core-aria2.service"], v), "ExecStart=/opt/bin/aria2c --conf-path=/srv/u/.config/media-core/aria2.conf") {
		t.Fatal("aria2 unit doesn't use the found aria2c")
	}
}
