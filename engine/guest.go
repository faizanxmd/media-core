package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Guests: people mint is shared with over Tailscale (another account's device).
// Tailscale tells us whose device a connection comes from, so no passwords are
// needed. A guest can watch, with their own watch progress, but can't reach
// Manager, downloads, the library's files or the settings. The owner's own
// devices and the home network are trusted as before.

type ctxKey int

const guestKey ctxKey = 0

var (
	cgnat = mustCIDR("100.64.0.0/10")       // Tailscale IPv4 addresses
	tsV6  = mustCIDR("fd7a:115c:a1e0::/48") // Tailscale IPv6 addresses
)

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

type gate struct {
	mu     sync.Mutex
	owner  string // the Tailscale login mint itself belongs to
	logins map[string]seen
	guests map[string]*Watch
}

type seen struct {
	login string
	at    time.Time
}

func newGate() *gate { return &gate{logins: map[string]seen{}, guests: map[string]*Watch{}} }

// tsLogin asks tailscaled whose device ip is ("" if it can't tell). A variable for tests.
var tsLogin = func(ip string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tailscale", "whois", "--json", ip).Output()
	if err != nil {
		return ""
	}
	var r struct{ UserProfile struct{ LoginName string } }
	if json.Unmarshal(out, &r) != nil {
		return ""
	}
	return r.UserProfile.LoginName
}

var ownerLogin = func() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil {
		return ""
	}
	var r struct {
		Self struct{ UserID json.Number }
		User map[string]struct{ LoginName string }
	}
	if json.Unmarshal(out, &r) != nil {
		return ""
	}
	return r.User[r.Self.UserID.String()].LoginName
}

// guestOf returns the guest's Tailscale login, or "" for the owner. A Tailscale
// connection whose owner can't be confirmed counts as a guest: it can still watch.
func (g *gate) guestOf(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !cgnat.Contains(ip) && !tsV6.Contains(ip) {
		return "" // this machine or the home network
	}
	g.mu.Lock()
	if g.owner == "" {
		g.mu.Unlock()
		o := ownerLogin()
		g.mu.Lock()
		g.owner = o
	}
	s, ok := g.logins[host]
	g.mu.Unlock()
	if !ok || time.Since(s.at) > 10*time.Minute {
		s = seen{tsLogin(host), time.Now()}
		g.mu.Lock()
		g.logins[host] = s
		g.mu.Unlock()
	}
	if s.login != "" && s.login == g.owner {
		return ""
	}
	if s.login == "" {
		return "unknown"
	}
	return s.login
}

// What a guest may use: the site itself, and the player's reading and progress calls.
var guestGET = []string{"/api/whoami", "/api/v1/hello", "/api/v1/home", "/api/v1/movies", "/api/v1/shows", "/api/v1/search",
	"/api/v1/item/", "/api/v1/stream/", "/api/v1/sub/", "/api/v1/thumbs/", "/api/v1/img/", "/api/v1/web"}

func guestAllowed(r *http.Request) bool {
	p := r.URL.Path
	if r.Method == http.MethodPost {
		return p == "/api/v1/progress" || p == "/api/v1/watched"
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if !strings.HasPrefix(p, "/api/") && !strings.HasPrefix(p, "/dav") {
		return true // the site's own files
	}
	for _, a := range guestGET {
		if p == a || strings.HasSuffix(a, "/") && strings.HasPrefix(p, a) || a == "/api/v1/web" && strings.HasPrefix(p, a+"/") {
			return true
		}
	}
	return false
}

func (g *gate) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guest := g.guestOf(r)
		if guest != "" {
			if !guestAllowed(r) {
				errJSON(w, http.StatusForbidden, "guests can watch, but not change anything")
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), guestKey, guest))
		}
		h.ServeHTTP(w, r)
	})
}

func guestFrom(r *http.Request) string {
	s, _ := r.Context().Value(guestKey).(string)
	return s
}

var reNotName = regexp.MustCompile(`[^a-zA-Z0-9._@-]+`)

// watch is the progress store for whoever is asking: the owner's, or the guest's own.
func (g *gate) watch(r *http.Request, owner *Watch) *Watch {
	who := guestFrom(r)
	if who == "" {
		return owner
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	w := g.guests[who]
	if w == nil {
		w = newWatchAt(filepath.Join(DataDir, "guests", reNotName.ReplaceAllString(who, "_")+".json"))
		g.guests[who] = w
	}
	return w
}
