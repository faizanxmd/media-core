package main

import (
	"net"
	"net/http"
)

// The site (web player and Manager) and the apps' API are open to this machine and the home network only.
// There are no accounts, so anything else is refused here, whatever the firewall allows: a port forwarded
// by mistake must not open the library to the internet. Reaching the server away from home is not
// supported yet; it will plug in here when it is.

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

type homeGate struct{}

func newHomeGate() *homeGate { return &homeGate{} }

func (homeGate) allowed(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return homeAddr(net.ParseIP(host))
}

func (g *homeGate) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.allowed(r) {
			http.Error(w, "This server is only open to its home network", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}
