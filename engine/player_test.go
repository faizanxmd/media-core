package main

import (
	"encoding/json"
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
