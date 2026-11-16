package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func newTestAuth(t *testing.T) (*devStore, *authGate, *pairing) {
	t.Helper()
	dir := t.TempDir()
	dev := loadDevices(filepath.Join(dir, "devices.json"))
	ag := &authGate{dev: dev}
	pr := newPairing(dev, ag)
	pr.setupPath = filepath.Join(dir, "setup.code")
	return dev, ag, pr
}

func jsonReq(method, path string, body any, tok string) *http.Request {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.RemoteAddr = "192.168.1.20:5000"
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	return r
}

// Every route the engine registers, read from its source, must refuse a request without a key, and every
// admin route must refuse a watching-only key. A new route can't be left open by accident.
func TestEveryRouteNeedsAKey(t *testing.T) {
	dev, ag, _ := newTestAuth(t)
	if _, _, err := dev.add("Owner", "web", roleAdmin); err != nil {
		t.Fatal(err)
	}
	playerTok, _, _ := dev.add("TV", "tv", rolePlayer)
	reached := false
	h := ag.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))

	files, _ := filepath.Glob("*.go")
	re := regexp.MustCompile(`HandleFunc\("([A-Z]+) (/[^"]*)"`)
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(f)
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			method := m[1]
			path := regexp.MustCompile(`\{[^}]*\}`).ReplaceAllString(m[2], "x")
			path = strings.TrimSuffix(path, "/x...") + "/x"
			if strings.HasSuffix(m[2], "{$}") {
				path = "/"
			}
			r := httptest.NewRequest(method, path, nil)
			if openRoute(r) {
				continue
			}
			n++
			reached = false
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != 401 || reached {
				t.Errorf("%s %s without a key: %d (reached=%v), want 401", method, path, rec.Code, reached)
			}
			if !playerRoute(path) {
				reached = false
				rec = httptest.NewRecorder()
				r = httptest.NewRequest(method, path, nil)
				r.Header.Set("Authorization", "Bearer "+playerTok)
				h.ServeHTTP(rec, r)
				if rec.Code != 403 || reached {
					t.Errorf("%s %s with a player key: %d, want 403", method, path, rec.Code)
				}
			}
		}
	}
	if n < 30 {
		t.Fatalf("only %d routes checked: is the route pattern still right?", n)
	}
}

func TestUnclaimedServerSaysSetup(t *testing.T) {
	_, ag, _ := newTestAuth(t)
	rec := httptest.NewRecorder()
	ag.wrap(http.NotFoundHandler()).ServeHTTP(rec, jsonReq("GET", "/api/v1/home", nil, ""))
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), `"setup"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestOpenHomeIsHomeOnly(t *testing.T) {
	dev, ag, _ := newTestAuth(t)
	dev.add("Owner", "web", roleAdmin)
	ag.openHome = true
	h := ag.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for addr, want := range map[string]int{"192.168.1.20:1": 204, "203.0.113.9:1": 401} {
		r := httptest.NewRequest("GET", "/api/v1/home", nil)
		r.RemoteAddr = addr
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", addr, rec.Code, want)
		}
	}
}

func TestPairing(t *testing.T) {
	dev, ag, pr := newTestAuth(t)
	adminTok, _, _ := dev.add("Owner", "web", roleAdmin)
	mux := http.NewServeMux()
	pr.mount(mux, true)
	h := ag.wrap(mux)
	call := func(method, path string, body any, tok string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, jsonReq(method, path, body, tok))
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	code, st := call("POST", "/api/v1/pair/start", map[string]string{"name": "Living room", "kind": "tv"}, "")
	if code != 200 || len(st["code"].(string)) != 7 || st["poll"] == "" {
		t.Fatalf("start: %d %v", code, st)
	}
	pairCode, poll := st["code"].(string), st["poll"].(string)
	if !strings.Contains(st["url"].(string), "/#/pair/"+normCode(pairCode)) {
		t.Fatalf("url = %v", st["url"])
	}

	if _, p := call("POST", "/api/v1/pair/poll", map[string]string{"poll": poll}, ""); p["state"] != "pending" {
		t.Fatalf("poll before approval: %v", p)
	}
	if code, _ := call("POST", "/api/v1/pair/poll", map[string]string{"poll": "guess"}, ""); code != 404 {
		t.Fatalf("a wrong poll secret: %d", code)
	}
	// the code alone can't approve: that takes an admin
	if code, _ := call("POST", "/api/pair/approve", map[string]string{"code": pairCode}, ""); code != 401 {
		t.Fatalf("approve without a key: %d", code)
	}
	if code, info := call("GET", "/api/pair/"+strings.ToLower(pairCode), nil, adminTok); code != 200 || info["name"] != "Living room" {
		t.Fatalf("info: %d %v", code, info)
	}
	if code, _ := call("POST", "/api/pair/approve", map[string]string{"code": pairCode}, adminTok); code != 200 {
		t.Fatalf("approve: %d", code)
	}
	_, p := call("POST", "/api/v1/pair/poll", map[string]string{"poll": poll}, "")
	tok, _ := p["token"].(string)
	if p["state"] != "approved" || !strings.HasPrefix(tok, "mc_") || p["role"] != rolePlayer {
		t.Fatalf("poll after approval: %v", p)
	}
	if _, again := call("POST", "/api/v1/pair/poll", map[string]string{"poll": poll}, ""); again["token"] != nil {
		t.Fatal("the key was handed out twice")
	}
	d := dev.lookup(tok)
	if d == nil || d.Name != "Living room" || d.Kind != "tv" {
		t.Fatalf("device = %+v", d)
	}
	// a player key can't approve others or list devices
	if code, _ := call("GET", "/api/devices", nil, tok); code != 403 {
		t.Fatalf("player listing devices: %d", code)
	}
	// revoked: refused on the next request
	if code, _ := call("POST", "/api/devices/"+d.ID+"/revoke", map[string]string{}, adminTok); code != 200 {
		t.Fatalf("revoke: %d", code)
	}
	if dev.lookup(tok) != nil {
		t.Fatal("revoked key still works")
	}
	// keys survive a restart, as hashes only
	raw, _ := os.ReadFile(dev.path)
	if strings.Contains(string(raw), adminTok) {
		t.Fatal("devices.json holds a key in plain text")
	}
	if loadDevices(dev.path).lookup(adminTok) == nil {
		t.Fatal("admin key lost on reload")
	}
}

func TestPairingDenyExpireAndLimits(t *testing.T) {
	_, _, pr := newTestAuth(t)
	start := func(ip string) (int, map[string]any) {
		r := jsonReq("POST", "/api/v1/pair/start", map[string]string{"kind": "phone"}, "")
		r.RemoteAddr = ip + ":1"
		rec := httptest.NewRecorder()
		pr.start(rec, r)
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	_, st := start("192.168.1.30")
	if !pr.deny(st["code"].(string)) {
		t.Fatal("deny failed")
	}
	rec := httptest.NewRecorder()
	pr.poll(rec, jsonReq("POST", "/api/v1/pair/poll", map[string]string{"poll": st["poll"].(string)}, ""))
	if !strings.Contains(rec.Body.String(), "denied") {
		t.Fatalf("denied poll: %s", rec.Body)
	}
	// expiry
	_, st = start("192.168.1.31")
	pr.mu.Lock()
	pr.byCode[normCode(st["code"].(string))].Created = time.Now().Add(-pairTTL - time.Second)
	pr.mu.Unlock()
	if _, err := pr.approve(st["code"].(string), "", "", "test"); err == nil {
		t.Fatal("an expired code was approved")
	}
	// at most 5 waiting
	for i := 0; i < pairMaxPending; i++ {
		start("192.168.1.4" + string(rune('0'+i)))
	}
	if code, _ := start("192.168.1.50"); code != 429 {
		t.Fatalf("6th waiting device: %d, want 429", code)
	}
	// at most 10 a minute from one address
	pr.mu.Lock()
	pr.byCode = map[string]*pendingPair{}
	pr.mu.Unlock()
	last := 0
	for i := 0; i < pairStartsPerM+1; i++ {
		last, _ = start("192.168.1.60")
		pr.mu.Lock()
		pr.byCode = map[string]*pendingPair{}
		pr.mu.Unlock()
	}
	if last != 429 {
		t.Fatalf("11th start in a minute: %d, want 429", last)
	}
}

func TestSetupClaim(t *testing.T) {
	dev, _, pr := newTestAuth(t)
	code, err := newSetupCode(pr.setupPath)
	if err != nil {
		t.Fatal(err)
	}
	claim := func(c string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		pr.claim(rec, jsonReq("POST", "/api/setup/claim", map[string]string{"code": c, "name": "Mac"}, ""))
		return rec
	}
	for i := 0; i < 9; i++ {
		if rec := claim("AAAA-AAAA"); rec.Code != 403 {
			t.Fatalf("wrong code: %d", rec.Code)
		}
	}
	if rec := claim("AAAA-AAAA"); rec.Code != 403 {
		t.Fatalf("10th wrong code: %d", rec.Code)
	}
	if rec := claim(code); rec.Code != 429 {
		t.Fatalf("right code while locked: %d, want 429", rec.Code)
	}
	pr.mu.Lock()
	pr.lockUntil = time.Time{}
	pr.mu.Unlock()
	rec := claim(strings.ToLower(strings.ReplaceAll(code, "-", " ")))
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Set-Cookie"), cookieName+"=mc_") {
		t.Fatalf("claim: %d %s %q", rec.Code, rec.Body, rec.Header().Get("Set-Cookie"))
	}
	if !dev.claimed() {
		t.Fatal("not claimed after the right code")
	}
	if _, err := os.Stat(pr.setupPath); err == nil {
		t.Fatal("setup code left behind")
	}
	if rec := claim(code); rec.Code != 409 {
		t.Fatalf("second claim: %d, want 409", rec.Code)
	}
}

func TestCLIToken(t *testing.T) {
	dev, _, _ := newTestAuth(t)
	p := filepath.Join(t.TempDir(), "cli.token")
	a := dev.cliToken(p)
	if dev.lookup(a) == nil || dev.lookup(a).Role != roleAdmin {
		t.Fatal("cli token isn't an admin key")
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("cli.token mode %v", fi.Mode().Perm())
	}
	if b := dev.cliToken(p); b != a {
		t.Fatal("a working cli token was replaced")
	}
	if dev.claimed() {
		t.Fatal("the command line alone counts as set up")
	}
}

func TestRemoteHook(t *testing.T) {
	g := newHomeGate()
	r := httptest.NewRequest("GET", "/api/v1/home", nil)
	r.RemoteAddr = "203.0.113.9:1"
	if g.allowed(r) {
		t.Fatal("internet address allowed with no remote piece")
	}
	RegisterRemote(fakeRemote{})
	defer RegisterRemote(nil)
	if !g.allowed(r) {
		t.Fatal("remote piece's trusted address refused")
	}
	if a := remoteAddrs(); len(a) != 1 || a[0] != "https://away.example.com" {
		t.Fatalf("remote addrs = %v", a)
	}
	if helloInfo()["remote"] == nil {
		t.Fatal("hello doesn't list remote addresses")
	}
}

type fakeRemote struct{}

func (fakeRemote) Trusted(r *http.Request, ip net.IP) bool { return ip.String() == "203.0.113.9" }
func (fakeRemote) Addrs() []string                         { return []string{"https://away.example.com"} }
