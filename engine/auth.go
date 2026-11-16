package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Every request needs a device key, apart from the few that find, set up or pair a device (openRoute).
// A key is "mc_" plus 32 random bytes; only its SHA-256 is kept, in devices.json. Apps send it as
// "Authorization: Bearer mc_…", browsers as the mc_session cookie (cookies ignore the port, so one sign-in
// covers the site and the apps' port). Revoking a device ends its key on its next request.
//
// Two roles: "player" (watch: /api/v1 and the web player's log) and "admin" (everything, including Manager,
// deleting, downloading and approving devices). The box's own command line has an admin key in cli.token.

const (
	roleAdmin  = "admin"
	rolePlayer = "player"
)

// cookieName: one per server (by its site port; both its ports share it), so a second server on the same
// computer, a temporary one say, doesn't overwrite this one's sign-in. Cookies don't keep ports apart.
func cookieName() string { return "mc_session_" + sitePort() }

type device struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"` // tv, phone, web, cli
	Role    string `json:"role"`
	Hash    string `json:"hash"`
	Created int64  `json:"created"`
	Seen    int64  `json:"seen"`
}

type devStore struct {
	mu     sync.Mutex
	path   string
	list   []*device
	byHash map[string]*device
	dirty  bool // Seen changed: written at most once a minute
}

type devFile struct {
	V       int       `json:"v"`
	Devices []*device `json:"devices"`
}

// loadDevices reads devices.json. A file that's there but can't be read is an error: treating it as empty
// would make the server look unclaimed and the next save would overwrite it.
func loadDevices(path string) (*devStore, error) {
	s := &devStore{path: path, byHash: map[string]*device{}}
	var f devFile
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("%s is damaged (%v). Fix it or move it away (every device then pairs again)", path, err)
		}
	}
	for _, d := range f.Devices {
		if d.Hash != "" && d.ID != "" {
			s.list = append(s.list, d)
			s.byHash[d.Hash] = d
		}
	}
	return s, nil
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func randToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the system has no randomness: nothing safe can be done
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// saveLocked writes devices.json atomically, readable only by this user.
func (s *devStore) saveLocked() error {
	b, err := json.MarshalIndent(devFile{V: 1, Devices: s.list}, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	s.dirty = false
	return os.Rename(tmp, s.path)
}

// add makes a device and returns its key (shown once, never stored).
func (s *devStore) add(name, kind, role string) (string, *device, error) {
	tok := "mc_" + randToken(32)
	now := time.Now().Unix()
	d := &device{ID: "d_" + randToken(6), Name: name, Kind: kind, Role: role, Hash: hashToken(tok), Created: now, Seen: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list = append(s.list, d)
	s.byHash[d.Hash] = d
	if err := s.saveLocked(); err != nil {
		s.list = s.list[:len(s.list)-1]
		delete(s.byHash, d.Hash)
		return "", nil, err
	}
	return tok, d, nil
}

// lookup finds the device a key belongs to (nil if none) and notes that it was seen.
func (s *devStore) lookup(tok string) *device {
	if !strings.HasPrefix(tok, "mc_") {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.byHash[hashToken(tok)]
	if d == nil {
		return nil
	}
	if now := time.Now().Unix(); now-d.Seen > 60 {
		d.Seen = now
		s.dirty = true
	}
	c := *d
	return &c
}

func (s *devStore) revoke(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range s.list {
		if d.ID == id {
			s.list = append(s.list[:i], s.list[i+1:]...)
			delete(s.byHash, d.Hash)
			s.saveLocked()
			return true
		}
	}
	return false
}

// removeKind drops every device of a kind (the old command-line key, when a new one is made).
func (s *devStore) removeKind(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := s.list[:0]
	for _, d := range s.list {
		if d.Kind == kind {
			delete(s.byHash, d.Hash)
			continue
		}
		keep = append(keep, d)
	}
	s.list = keep
	s.saveLocked()
}

// lastAdmin: id is the only admin besides the command line (removing it would leave nobody to manage the server).
func (s *devStore) lastAdmin(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	admins, isAdmin := 0, false
	for _, d := range s.list {
		if d.Role == roleAdmin && d.Kind != "cli" {
			admins++
			if d.ID == id {
				isAdmin = true
			}
		}
	}
	return isAdmin && admins == 1
}

// prove lets an app check that it's talking to its own server before it sends its key: the app sends a
// random nonce and the start of its key's hash (k, 16 hex), and the server answers HMAC-SHA256 of the nonce
// keyed by the whole hash, which only the real server (and the app) can work out. The key itself never travels.
func (s *devStore) prove(w http.ResponseWriter, r *http.Request) {
	k, n := r.URL.Query().Get("k"), r.URL.Query().Get("n")
	if len(k) != 16 || len(n) < 16 || len(n) > 128 {
		errJSON(w, 400, "k (16 hex) and n (a nonce) are needed")
		return
	}
	s.mu.Lock()
	var hash string
	for _, d := range s.list {
		if d.Kind != "cli" && strings.HasPrefix(d.Hash, k) {
			hash = d.Hash
			break
		}
	}
	s.mu.Unlock()
	if hash == "" {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "unknown"})
		return
	}
	m := hmac.New(sha256.New, []byte(hash))
	m.Write([]byte(n))
	writeJSON(w, 200, map[string]any{"mac": hex.EncodeToString(m.Sum(nil))})
}

// claimed: someone has set the server up (an admin device other than the command line exists).
func (s *devStore) claimed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.list {
		if d.Role == roleAdmin && d.Kind != "cli" {
			return true
		}
	}
	return false
}

type deviceView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Role    string `json:"role"`
	Created int64  `json:"created"`
	Seen    int64  `json:"seen"`
	This    bool   `json:"this,omitempty"` // the device asking
}

func (s *devStore) views(me string) []deviceView {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]deviceView, 0, len(s.list))
	for _, d := range s.list {
		out = append(out, deviceView{d.ID, d.Name, d.Kind, d.Role, d.Created, d.Seen, d.ID == me})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seen > out[j].Seen })
	return out
}

func (s *devStore) flushSeen() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirty {
		s.saveLocked()
	}
}

// cliToken makes sure the command line has a working admin key in cli.token (0600) and returns it.
func (s *devStore) cliToken(path string) string {
	if b, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(b)); s.lookup(tok) != nil {
			return tok
		}
	}
	s.removeKind("cli")
	tok, _, err := s.add("Command line on the server", "cli", roleAdmin)
	if err != nil {
		return ""
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte(tok+"\n"), 0o600)
	return tok
}

// ---------- the check on every request ----------

type ctxKey struct{}

// deviceFrom is the device a request was made with (nil on open routes).
func deviceFrom(r *http.Request) *device {
	d, _ := r.Context().Value(ctxKey{}).(*device)
	return d
}

func requestToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	if c, err := r.Cookie(cookieName()); err == nil {
		return c.Value
	}
	return ""
}

// openRoute: reachable without a key (still home network only). Finding the server, its download page and
// app files, setting it up and pairing; the site's own files (pages, scripts, fonts); WebDAV, which has its
// own password.
func openRoute(r *http.Request) bool {
	p := r.URL.Path
	switch p {
	case "/api/v1/hello", "/api/v1/pair/start", "/api/v1/pair/poll", "/api/v1/prove",
		"/api/v1/app", "/api/v1/phone-app", "/api/v1/app.apk", "/api/v1/phone-app.apk",
		"/api/setup/state", "/api/setup/claim":
		return true
	}
	if p == "/dav" || strings.HasPrefix(p, "/dav/") {
		return true
	}
	return !strings.HasPrefix(p, "/api/") // pages and static files; on the apps' port, the download page
}

// playerRoute: what a watching-only device may use. Everything else under /api/ needs an admin.
func playerRoute(p string) bool {
	return strings.HasPrefix(p, "/api/v1/") || p == "/api/weblog"
}

type authGate struct {
	dev   *devStore
	names sync.Map // client IP -> device name, for "Playing now"

	liveMu sync.Mutex
	live   map[string]map[*int]context.CancelFunc // device ID -> its requests still running (streams, events)
}

// endSessions cuts off every request a device still has running: removing a device ends its stream now,
// not when it next asks for something.
func (a *authGate) endSessions(id string) {
	a.liveMu.Lock()
	defer a.liveMu.Unlock()
	for _, cancel := range a.live[id] {
		cancel()
	}
	delete(a.live, id)
}

func (a *authGate) track(id string, cancel context.CancelFunc) func() {
	k := new(int)
	a.liveMu.Lock()
	if a.live == nil {
		a.live = map[string]map[*int]context.CancelFunc{}
	}
	if a.live[id] == nil {
		a.live[id] = map[*int]context.CancelFunc{}
	}
	a.live[id][k] = cancel
	a.liveMu.Unlock()
	return func() {
		a.liveMu.Lock()
		delete(a.live[id], k)
		if len(a.live[id]) == 0 {
			delete(a.live, id)
		}
		a.liveMu.Unlock()
		cancel()
	}
}

func (a *authGate) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if openRoute(r) {
			h.ServeHTTP(w, r)
			return
		}
		d := a.dev.lookup(requestToken(r))
		if d == nil {
			if !a.dev.claimed() {
				writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "setup",
					"message": "This server isn't set up yet. Open its website: http://<server>:" + sitePort() + "/#/setup"})
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "pair",
				"message": "This device isn't paired with the server."})
			return
		}
		if d.Role != roleAdmin && !playerRoute(r.URL.Path) {
			writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "admin",
				"message": "Only an admin device can do this."})
			return
		}
		if ip := clientIP(r); ip != nil {
			a.names.Store(ip.String(), d.Name)
		}
		ctx, cancel := context.WithCancel(context.WithValue(r.Context(), ctxKey{}, d))
		done := a.track(d.ID, cancel)
		defer done()
		// A revoke between the lookup and track would miss this request: look again now that it's tracked.
		if again := a.dev.lookup(requestToken(r)); again == nil || again.ID != d.ID {
			cancel()
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "pair",
				"message": "This device isn't paired with the server."})
			return
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func clientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// nameOf: the name of the device last seen at an address ("" if none).
func (a *authGate) nameOf(host string) string {
	if v, ok := a.names.Load(host); ok {
		return v.(string)
	}
	return ""
}

func setSessionCookie(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{Name: cookieName(), Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: 400 * 24 * 3600})
}

func cliTokenPath() string { return filepath.Join(ConfDir, "cli.token") }
