package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	dev, err := loadDevices(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
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

// No way in without a key, not even from the home network.
func TestUnpairedHomeDeviceIsRefused(t *testing.T) {
	dev, ag, _ := newTestAuth(t)
	dev.add("Owner", "web", roleAdmin)
	t.Setenv("MEDIA_CORE_OPEN_HOME", "1") // the old development switch does nothing now
	h := ag.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for addr, want := range map[string]int{"10.0.0.20:1": 401, "127.0.0.1:1": 401, "203.0.113.9:1": 401} {
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
	if d2, _ := loadDevices(dev.path); d2.lookup(adminTok) == nil {
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
	// at most pairMaxPerAddr waiting from one address
	pr.mu.Lock()
	pr.byCode = map[string]*pendingPair{}
	pr.mu.Unlock()
	for i := 0; i < pairMaxPerAddr; i++ {
		start("192.168.1.60")
	}
	if code, _ := start("192.168.1.60"); code != 429 {
		t.Fatalf("one address over its limit: %d, want 429", code)
	}
	// at most pairMaxPending from everyone
	pr.mu.Lock()
	pr.byCode = map[string]*pendingPair{}
	pr.mu.Unlock()
	for i := 0; i < pairMaxPending; i++ {
		start(fmt.Sprintf("10.0.0.%d", i+1))
	}
	if code, _ := start("10.0.0.200"); code != 429 {
		t.Fatalf("one more waiting device than allowed: %d, want 429", code)
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
	// another address isn't locked out by this one's wrong codes
	other := jsonReq("POST", "/api/setup/claim", map[string]string{"code": "AAAA-AAAA"}, "")
	other.RemoteAddr = "192.168.1.77:5000"
	if rec := httptest.NewRecorder(); func() int { pr.claim(rec, other); return rec.Code }() != 403 {
		t.Fatal("one address's wrong codes locked another out")
	}
	pr.mu.Lock()
	pr.lockUntil = map[string]time.Time{}
	pr.mu.Unlock()
	rec := claim(strings.ToLower(strings.ReplaceAll(code, "-", " ")))
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Set-Cookie"), cookieName()+"=mc_") {
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

// An app checks its server knows its key before sending it: the answer is HMAC(nonce) keyed by the key's hash.
func TestProve(t *testing.T) {
	dev, _, _ := newTestAuth(t)
	tok, _, _ := dev.add("TV", "tv", rolePlayer)
	h := hashToken(tok)
	ask := func(k, n string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		dev.prove(rec, httptest.NewRequest("GET", "/api/v1/prove?k="+k+"&n="+n, nil))
		return rec
	}
	nonce := "0123456789abcdef0123"
	rec := ask(h[:16], nonce)
	m := hmac.New(sha256.New, []byte(h))
	m.Write([]byte(nonce))
	if want := hex.EncodeToString(m.Sum(nil)); !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("proof: %d %s", rec.Code, rec.Body)
	}
	if rec := ask("0000000000000000", nonce); rec.Code != 404 {
		t.Fatalf("unknown key: %d", rec.Code)
	}
	if rec := ask(h[:16], "short"); rec.Code != 400 {
		t.Fatalf("short nonce: %d", rec.Code)
	}
}

// The only admin can't be removed: nobody could manage the server, and it would have no setup code.
func TestLastAdminStays(t *testing.T) {
	dev, ag, pr := newTestAuth(t)
	adminTok, a, _ := dev.add("Mac", "web", roleAdmin)
	mux := http.NewServeMux()
	pr.mount(mux, true)
	h := ag.wrap(mux)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq("POST", "/api/devices/"+a.ID+"/revoke", nil, adminTok))
	if rec.Code != 409 {
		t.Fatalf("last admin revoked: %d", rec.Code)
	}
	_, b, _ := dev.add("Phone", "phone", roleAdmin)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq("POST", "/api/devices/"+b.ID+"/revoke", nil, adminTok))
	if rec.Code != 200 {
		t.Fatalf("second admin: %d", rec.Code)
	}
}

// A browser's key arrives only as its HttpOnly cookie; an app's in the answer. The key is made when collected.
func TestPairKeyMadeOnCollect(t *testing.T) {
	dev, _, pr := newTestAuth(t)
	for _, kind := range []string{"web", "tv"} {
		rec := httptest.NewRecorder()
		pr.start(rec, jsonReq("POST", "/api/v1/pair/start", map[string]string{"kind": kind}, ""))
		var st map[string]any
		json.Unmarshal(rec.Body.Bytes(), &st)
		before := len(dev.views(""))
		if _, err := pr.approve(st["code"].(string), "", "", "test"); err != nil {
			t.Fatal(err)
		}
		if len(dev.views("")) != before {
			t.Fatal("a key was made before the device collected it")
		}
		rec = httptest.NewRecorder()
		pr.poll(rec, jsonReq("POST", "/api/v1/pair/poll", map[string]string{"poll": st["poll"].(string)}, ""))
		hasTok := strings.Contains(rec.Body.String(), `"token"`)
		hasCookie := strings.Contains(rec.Header().Get("Set-Cookie"), cookieName()+"=mc_")
		if kind == "web" && (hasTok || !hasCookie) || kind == "tv" && (!hasTok || hasCookie) {
			t.Fatalf("%s: token in body %v, cookie %v", kind, hasTok, hasCookie)
		}
		if len(dev.views("")) != before+1 {
			t.Fatalf("%s: no device after collecting", kind)
		}
	}
}

// A setting with a line break would add lines of its own to engine.env.
func TestSetEnvLineRefusesLineBreaks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "engine.env")
	if err := setEnvLine(p, "MEDIA_CORE_LIBRARY", "/x\nMEDIA_CORE_OTHER=1"); err == nil {
		t.Fatal("a value with a line break was written")
	}
}

// Removing a device ends the requests it still has running (a stream, the events feed).
func TestRevokeEndsRunningRequests(t *testing.T) {
	dev, ag, pr := newTestAuth(t)
	adminTok, _, _ := dev.add("Mac", "web", roleAdmin)
	tvTok, tv, _ := dev.add("TV", "tv", rolePlayer)
	ended := make(chan struct{})
	started := make(chan struct{})
	mux := http.NewServeMux()
	pr.mount(mux, true)
	mux.HandleFunc("GET /api/v1/stream/x", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(ended)
	})
	h := ag.wrap(mux)
	go h.ServeHTTP(httptest.NewRecorder(), jsonReq("GET", "/api/v1/stream/x", nil, tvTok))
	<-started
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq("POST", "/api/devices/"+tv.ID+"/revoke", nil, adminTok))
	if rec.Code != 200 {
		t.Fatalf("revoke: %d", rec.Code)
	}
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the removed device's stream kept running")
	}
}
