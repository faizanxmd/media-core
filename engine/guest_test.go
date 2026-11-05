package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuestAllowed(t *testing.T) {
	ok := [][2]string{{"GET", "/"}, {"GET", "/app.js"}, {"GET", "/api/whoami"}, {"GET", "/api/v1/home"}, {"GET", "/api/v1/item/abc"},
		{"GET", "/api/v1/stream/abc"}, {"HEAD", "/api/v1/stream/abc"}, {"GET", "/api/v1/web"}, {"GET", "/api/v1/web/abc/x0.vtt"},
		{"POST", "/api/v1/progress"}, {"POST", "/api/v1/watched"}, {"GET", "/api/v1/img/w185/x.jpg"}}
	no := [][2]string{{"GET", "/api/jobs"}, {"GET", "/api/events"}, {"POST", "/api/probe"}, {"POST", "/api/jobs"},
		{"POST", "/api/library/delete"}, {"GET", "/api/library"}, {"GET", "/api/manager/titles"}, {"POST", "/api/manager/mp4"},
		{"GET", "/api/system"}, {"POST", "/api/settings"}, {"POST", "/api/v1/match"}, {"GET", "/api/v1/app.apk"},
		{"GET", "/dav/"}, {"PROPFIND", "/dav/"}, {"DELETE", "/api/v1/stream/abc"}, {"GET", "/api/v1/webx"}}
	for _, c := range ok {
		if !guestAllowed(httptest.NewRequest(c[0], c[1], nil)) {
			t.Errorf("guest should reach %s %s", c[0], c[1])
		}
	}
	for _, c := range no {
		if guestAllowed(httptest.NewRequest(c[0], c[1], nil)) {
			t.Errorf("guest shouldn't reach %s %s", c[0], c[1])
		}
	}
}

func TestHomeIsOwner(t *testing.T) {
	g := newGate()
	for _, a := range []string{"192.168.68.104:5000", "127.0.0.1:5000", "[::1]:5000"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = a
		if who := g.guestOf(r); who != "" {
			t.Errorf("%s counted as guest %q", a, who)
		}
	}
}

func TestGuestThroughTheGate(t *testing.T) {
	oldT, oldO, oldD := tsLogin, ownerLogin, DataDir
	tsLogin = func(ip string) string {
		if ip == "100.75.19.19" {
			return "me@github"
		}
		return "friend@github"
	}
	ownerLogin = func() string { return "me@github" }
	DataDir = t.TempDir()
	defer func() { tsLogin, ownerLogin, DataDir = oldT, oldO, oldD }()

	g := newGate()
	owner := &Watch{m: map[string]*WState{}}
	var seen *Watch
	h := g.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = g.watch(r, owner); w.WriteHeader(200) }))
	call := func(method, path, from string) int {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = from + ":4000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	if c := call("GET", "/api/jobs", "100.75.19.19"); c != 200 || seen != owner {
		t.Fatalf("owner over Tailscale: %d", c)
	}
	if c := call("GET", "/api/jobs", "100.101.1.2"); c != 403 {
		t.Fatalf("guest reached downloads: %d", c)
	}
	if c := call("POST", "/api/library/delete", "100.101.1.2"); c != 403 {
		t.Fatalf("guest reached delete: %d", c)
	}
	if c := call("GET", "/api/v1/home", "100.101.1.2"); c != 200 || seen == owner || seen == nil {
		t.Fatalf("guest home: %d, own progress: %v", c, seen != owner)
	}
	seen.Progress("film", 600_000, 6_000_000, false, 0)
	if owner.Get("film") != nil {
		t.Fatal("a guest's progress landed in the owner's")
	}
}
