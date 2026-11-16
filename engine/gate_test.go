package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHomeGate(t *testing.T) {
	h := newHomeGate().wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for from, want := range map[string]int{
		"127.0.0.1:4000":           200, // this machine
		"[::1]:4000":               200,
		"192.168.1.10:4000":        200, // home network
		"10.0.0.5:4000":            200,
		"172.16.4.2:4000":          200,
		"[fe80::1]:4000":           200, // link-local
		"[fd12:3456::1]:4000":      200, // home IPv6 (ULA)
		"100.64.0.10:4000":         403, // Tailscale: away from home isn't supported yet
		"[fd7a:115c:a1e0::1]:4000": 403,
		"203.0.113.7:4000":         403, // the internet
		"[2001:db8::1]:4000":       403,
		"garbage":                  403,
	} {
		r := httptest.NewRequest("POST", "/api/library/delete", nil)
		r.RemoteAddr = from
		r.Host = "192.168.1.5:8780"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Errorf("%s: got %d, want %d", from, rec.Code, want)
		}
	}
}

// A page on the internet that points its own name at a home address (DNS rebinding) is refused by name.
func TestHomeHost(t *testing.T) {
	t.Setenv("MEDIA_CORE_HOSTS", "media.example.org")
	for host, want := range map[string]bool{
		"192.168.1.5:8780": true, "[fe80::1]:8790": true, "localhost:8780": true, "mint:8780": true,
		"mint.local": true, "box.lan:8780": true, "nas.home.arpa": true, "media.example.org:8780": true,
		"evil.example.com": false, "rebind.attacker.net:8780": false, "": true,
	} {
		if got := homeHost(host); got != want {
			t.Errorf("%q: got %v, want %v", host, got, want)
		}
	}
}
