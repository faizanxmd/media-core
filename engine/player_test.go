package main

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func resetMagicDNSCache(t *testing.T) {
	t.Helper()
	magicDNSMu.Lock()
	magicDNSName = ""
	magicDNSMu.Unlock()
	t.Cleanup(func() {
		magicDNSMu.Lock()
		magicDNSName = ""
		magicDNSMu.Unlock()
	})
}

func TestMagicDNSAndHelloInfo(t *testing.T) {
	resetMagicDNSCache(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("MEDIA_CORE_PUBLIC_HOST", "")
	t.Setenv("MEDIA_CORE_NAME", "Cinema")
	t.Setenv("MEDIA_CORE_PLAYER_LISTEN", "0.0.0.0:9790")

	if got := magicDNS(); got != "" {
		t.Fatalf("magicDNS() without host/tailscale = %q, want empty", got)
	}

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
	if _, has := raw["magicdns"]; has {
		t.Fatalf("hello unexpectedly included magicdns=%v when magicDNS() is empty", raw["magicdns"])
	}
	for _, k := range []string{"lan", "tailscale"} {
		if xs, ok := raw[k].([]any); ok {
			for _, u := range xs {
				if !strings.HasSuffix(u.(string), ":9790") {
					t.Fatalf("%s url = %q, want :9790 port", k, u)
				}
			}
		}
	}

	t.Setenv("MEDIA_CORE_PUBLIC_HOST", "home.example.com.")
	if got := magicDNS(); got != "home.example.com" {
		t.Fatalf("magicDNS() with public host = %q, want home.example.com", got)
	}
	info := helloInfo()
	if info["magicdns"] != "http://home.example.com:9790" {
		t.Fatalf("helloInfo magicdns = %v, want http://home.example.com:9790", info["magicdns"])
	}
}

func TestConnectPageDeepLink(t *testing.T) {
	info := map[string]any{
		"name":      "living-room",
		"lan":       []string{"http://192.168.1.10:8790", "http://10.0.0.5:8790"},
		"tailscale": []string{"http://100.64.0.10:8790"},
		"magicdns":  "http://box.example.com:8790",
	}
	wantLink := "mediacore://connect?" +
		"u=http%3A%2F%2F192.168.1.10%3A8790" +
		"&u=http%3A%2F%2F10.0.0.5%3A8790" +
		"&u=http%3A%2F%2F100.64.0.10%3A8790" +
		"&u=http%3A%2F%2Fbox.example.com%3A8790"

	if got := connectDeepLink(info); got != wantLink {
		t.Fatalf("connectDeepLink = %q, want %q", got, wantLink)
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

	// Without magicdns, no trailing empty u= parameter.
	noMDNS := map[string]any{
		"name": "den",
		"lan":  []string{"http://192.168.1.10:8790"},
	}
	if got := connectDeepLink(noMDNS); got != "mediacore://connect?u=http%3A%2F%2F192.168.1.10%3A8790" {
		t.Fatalf("connectDeepLink without magicdns = %q", got)
	}
}

func TestNeutralServerStrings(t *testing.T) {
	t.Setenv("MEDIA_CORE_NAME", "Studio")
	if got, relayed := deviceOf("127.0.0.1"); got != "Studio itself" || relayed {
		t.Fatalf("deviceOf(127.0.0.1) = (%q, %v), want (%q, false)", got, relayed, "Studio itself")
	}

	oldT, oldO := tsLogin, ownerLogin
	tsLogin = func(string) string { return "other@github" }
	ownerLogin = func() string { return "me@github" }
	defer func() { tsLogin, ownerLogin = oldT, oldO }()

	h := newOwnerGate().wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest("GET", "/api/v1/home", nil)
	req.RemoteAddr = "100.64.0.20:4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 || strings.TrimSpace(rec.Body.String()) != "This server is only open to its owner's devices" {
		t.Fatalf("403 response = (%d, %q)", rec.Code, rec.Body.String())
	}
}
