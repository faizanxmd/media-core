package main

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelloInfo(t *testing.T) {
	t.Setenv("MEDIA_CORE_NAME", "Cinema")
	t.Setenv("MEDIA_CORE_PLAYER_LISTEN", "0.0.0.0:9790")

	p := &Player{c: &Catalog{items: map[string]*Item{}}, w: newTestWatch(), t: &TMDB{}}
	rec := httptest.NewRecorder()
	p.hello(rec, httptest.NewRequest("GET", "/api/v1/hello", nil))
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["name"] != "Cinema" {
		t.Fatalf("hello name = %v, want Cinema", raw["name"])
	}
	for _, k := range []string{"tailscale", "magicdns"} {
		if _, has := raw[k]; has {
			t.Fatalf("hello has %q; away-from-home addresses aren't served", k)
		}
	}
	if xs, ok := raw["lan"].([]any); ok {
		for _, u := range xs {
			if !strings.HasSuffix(u.(string), ":9790") {
				t.Fatalf("lan url = %q, want :9790 port", u)
			}
		}
	}
}

func TestConnectPageDeepLink(t *testing.T) {
	info := map[string]any{
		"name": "living-room",
		"lan":  []string{"http://192.168.1.10:8790", "http://10.0.0.5:8790"},
	}
	wantLink := "mediacore://connect?" +
		"u=http%3A%2F%2F192.168.1.10%3A8790" +
		"&u=http%3A%2F%2F10.0.0.5%3A8790"

	if got := connectDeepLink(info); got != wantLink {
		t.Fatalf("connectDeepLink = %q, want %q", got, wantLink)
	}

	// a fresh server has no app builds: no links to files that aren't there
	if page := string(renderConnectPage(info)); strings.Contains(page, "app.apk") || !strings.Contains(page, "No app build") {
		t.Fatalf("a server with no apps links to them:\n%s", page)
	}
	appDir := filepath.Join(DataDir, "app")
	os.MkdirAll(appDir, 0o755)
	t.Cleanup(func() { os.RemoveAll(appDir) })
	for _, n := range []string{"media-core", "media-core-phone"} {
		os.WriteFile(filepath.Join(appDir, n+".json"), []byte(`{"version_code":1,"version_name":"1.0.0","size":1}`), 0o644)
	}
	page := string(renderConnectPage(info))
	unescaped := html.UnescapeString(page)
	for _, want := range []string{
		"<title>Media Core</title>",
		"living-room",
		`href="/api/v1/app.apk"`,
		"Download the TV app",
		`href="/api/v1/phone-app.apk"`,
		"Download the phone app",
		"Open in Media Core",
		`href="` + wantLink + `"`,
		"Install the app first, then tap this to connect it to living-room.",
	} {
		if !strings.Contains(unescaped, want) {
			t.Fatalf("connect page missing %q\npage:\n%s", want, page)
		}
	}

}

func TestNeutralServerStrings(t *testing.T) {
	t.Setenv("MEDIA_CORE_NAME", "Studio")
	if got := deviceOf("127.0.0.1"); got != "Studio itself" {
		t.Fatalf("deviceOf(127.0.0.1) = %q, want %q", got, "Studio itself")
	}

	h := newHomeGate().wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest("GET", "/api/v1/home", nil)
	req.RemoteAddr = "100.64.0.20:4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 || strings.TrimSpace(rec.Body.String()) != "This server is only open to its home network" {
		t.Fatalf("403 response = (%d, %q)", rec.Code, rec.Body.String())
	}
}
