# Away from home: the hook

Media Core works on the home network only. Reaching it from elsewhere (over Tailscale, WireGuard, a tunnel) is left to an add-on, which plugs into one hook in `engine/gate.go`. Nothing else in the engine changes.

```go
type Remote interface {
	// Trusted says whether a request that isn't from the home network may in, given its source address.
	// Return true only for a path you control end to end (your tailnet, your tunnel).
	Trusted(r *http.Request, ip net.IP) bool
	// Addrs are base URLs the apps can try away from home, like "https://box.example.ts.net:8790".
	Addrs() []string
}

func RegisterRemote(r Remote)
```

What follows from registering one:

- **The gate.** A request from outside the home network goes in only if `Trusted` says yes. Home-network requests never ask it.
- **Host names.** The host names in `Addrs()` are allowed by the DNS-rebinding check, like home-network names.
- **The device key still applies.** `Trusted` only lets a request reach the second check. Every device still needs its key, and roles still apply.
- **The apps.** `/api/v1/hello` lists `Addrs()` as `"remote"`. The apps add them to the addresses they race, prefer a home-network address whenever one answers, and send their key only after the server proves it knows it.
- **Speed.** The apps treat a non-home address as a thin link and pick lighter copies where there are some.

An add-on should run TLS (or a network that encrypts, such as WireGuard), since keys travel in every request. Register once, early in `serve`, before the listeners start.
