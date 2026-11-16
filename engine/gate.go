package main

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// The first of two checks on every request: the network. The site and the apps' API answer this machine and
// the home network only, whatever the firewall allows, so a port forwarded by mistake doesn't open the
// library to the internet. Reaching the server away from home plugs in here (Remote). The second check,
// the device key, is in auth.go.

var (
	// Tailscale's IPv6 range sits inside fc00::/7 (which IsPrivate accepts), so it is named and refused.
	tsV6 = mustCIDR("fd7a:115c:a1e0::/48")
)

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// homeAddr: loopback, a private home-network address (10/8, 172.16/12, 192.168/16, fc00::/7) or link-local.
func homeAddr(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return true
	}
	return ip.IsPrivate() && !tsV6.Contains(ip)
}

// Remote is where reaching the server away from home plugs in, and the only place. It may let an address
// outside the home network through this check (the device key is still checked after it), and it may add
// addresses the apps can try, which /api/v1/hello lists as "remote". Without one, the server is home-only.
type Remote interface {
	Trusted(r *http.Request, ip net.IP) bool
	Addrs() []string
}

var (
	remoteMu sync.RWMutex
	remote   Remote
)

// RegisterRemote installs the away-from-home piece (call it before serving).
func RegisterRemote(r Remote) {
	remoteMu.Lock()
	remote = r
	remoteMu.Unlock()
}

func currentRemote() Remote {
	remoteMu.RLock()
	defer remoteMu.RUnlock()
	return remote
}

// remoteAddrs: the extra addresses the away-from-home piece offers (none without one).
func remoteAddrs() []string {
	if rm := currentRemote(); rm != nil {
		return rm.Addrs()
	}
	return nil
}

type homeGate struct{}

func newHomeGate() *homeGate { return &homeGate{} }

func (homeGate) allowed(r *http.Request) bool {
	ip := clientIP(r)
	if homeAddr(ip) {
		return true
	}
	rm := currentRemote()
	return ip != nil && rm != nil && rm.Trusted(r, ip)
}

func (g *homeGate) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.allowed(r) {
			http.Error(w, "This server is only open to its home network", http.StatusForbidden)
			return
		}
		if !homeHost(r.Host) {
			http.Error(w, "Open this server by its address or its name on the home network", http.StatusMisdirectedRequest)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// homeHost: the name a request was sent to is one a home network uses (an IP address, a one-word name, or a
// local suffix such as .local or .lan), or one listed in MEDIA_CORE_HOSTS. A web page on the internet can point
// its own name at a home address (DNS rebinding) to reach this server from someone's browser; its name is
// refused here. A remote-access add-on adds the names it serves on.
func homeHost(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	h = strings.ToLower(strings.TrimSuffix(strings.Trim(h, "[]"), "."))
	if h == "" || net.ParseIP(h) != nil || !strings.Contains(h, ".") {
		return true
	}
	for _, suf := range []string{".local", ".lan", ".home", ".home.arpa", ".internal", ".localdomain", ".localhost"} {
		if strings.HasSuffix(h, suf) {
			return true
		}
	}
	for _, x := range strings.Fields(strings.ReplaceAll(envOr("MEDIA_CORE_HOSTS", ""), ",", " ")) {
		if strings.EqualFold(strings.TrimSuffix(x, "."), h) {
			return true
		}
	}
	for _, x := range remoteHosts() {
		if strings.EqualFold(x, h) {
			return true
		}
	}
	return false
}

// remoteHosts: the host names in the remote add-on's addresses.
func remoteHosts() []string {
	var out []string
	for _, a := range remoteAddrs() {
		if u, err := url.Parse(a); err == nil && u.Hostname() != "" {
			out = append(out, u.Hostname())
		}
	}
	return out
}
