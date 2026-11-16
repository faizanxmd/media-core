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
		case "100.64.0.10":
			return "me@github"
		case "100.64.0.20":
			return "friend@github"
		}
		return ""
	}
	ownerLogin = func() string { return "me@github" }
	defer func() { tsLogin, ownerLogin = oldT, oldO }()

	h := newOwnerGate().wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for from, want := range map[string]int{
		"127.0.0.1:4000":    200, // this machine
		"[::1]:4000":        200,
		"192.168.1.10:4000": 200, // home network
		"100.64.0.10:4000":  200, // the owner's device on Tailscale
		"100.64.0.20:4000":  403, // someone the server was shared with
		"100.64.0.21:4000":  403, // Tailscale, owner unknown
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
