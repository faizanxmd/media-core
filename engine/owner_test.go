package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOwnerGate(t *testing.T) {
	oldT, oldO := tsLogin, ownerLogin
	tsLogin = func(ip string) string {
		switch ip {
		case "100.75.19.19":
			return "me@github"
		case "100.90.0.1":
			return "friend@github"
		}
		return ""
	}
	ownerLogin = func() string { return "me@github" }
	defer func() { tsLogin, ownerLogin = oldT, oldO }()

	h := newOwnerGate().wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for from, want := range map[string]int{
		"127.0.0.1:4000":      200, // this machine
		"[::1]:4000":          200,
		"192.168.68.104:4000": 200, // home network
		"100.75.19.19:4000":   200, // the owner's device on Tailscale
		"100.90.0.1:4000":     403, // someone mint was shared with
		"100.90.0.2:4000":     403, // Tailscale, owner unknown
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
