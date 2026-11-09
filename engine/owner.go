package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

// The :8780 site (web player and Manager) is for the owner only. mint is
// reachable over Tailscale, and if it's ever shared with another account, that
// account's devices would otherwise get everything, Manager's delete included.
// Tailscale tells us whose device a connection comes from; anything on
// Tailscale that isn't the owner's is turned away. This machine and the home
// network are trusted as before.

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

type ownerGate struct {
	mu     sync.Mutex
	owner  string // the Tailscale login mint itself belongs to
	logins map[string]seen
}

type seen struct {
	login string
	at    time.Time
}

func newOwnerGate() *ownerGate { return &ownerGate{logins: map[string]seen{}} }

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

// allowed: this machine, the home network, or one of the owner's Tailscale
// devices. A Tailscale connection whose owner can't be confirmed is refused.
func (g *ownerGate) allowed(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !cgnat.Contains(ip) && !tsV6.Contains(ip) {
		return true
	}
	g.mu.Lock()
	if g.owner == "" {
		g.mu.Unlock()
		o := ownerLogin()
		g.mu.Lock()
		g.owner = o
	}
	s, ok := g.logins[host]
	owner := g.owner
	g.mu.Unlock()
	if !ok || time.Since(s.at) > 10*time.Minute {
		s = seen{tsLogin(host), time.Now()}
		g.mu.Lock()
		g.logins[host] = s
		g.mu.Unlock()
	}
	return s.login != "" && s.login == owner
}

func (g *ownerGate) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.allowed(r) {
			http.Error(w, "mint isn't shared", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}
