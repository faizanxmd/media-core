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
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Errorf("%s: got %d, want %d", from, rec.Code, want)
		}
	}
}
