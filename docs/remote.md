# Away from home: the hook

Version 1 works on the home network only, on purpose: the server speaks plain HTTP, and pairing assumes the people on the network are the household. Reaching it from elsewhere (over Tailscale, WireGuard, a tunnel) is left to an add-on, which plugs into one hook in `engine/gate.go`. Nothing else in the engine changes.

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

## What an add-on must provide

- **Encryption.** Device keys and cookies travel with every request, so they must never cross the internet in clear: TLS, or a network that encrypts end to end.
- **Its own way in.** A listener or a tunnel of its own. The engine's two ports stay on the home network.
- **Limits from outside.** Rate limiting on pairing and on setup codes for requests that come through it.

## What an add-on must not do

- Let a device start pairing from outside without an extra step of its own (approving on a home device is not enough when anyone on the internet can ask).
- Skip the device key. `Trusted` lets a request reach the key check; it never replaces it.
- Trust an `X-Forwarded-For` or similar header from a hop it doesn't control.
- Block in `Trusted`. It runs on every outside request and must answer from memory.

## How it ships

As a separate build that imports the hook, not as a setting in the main binary. The release binary has no `Remote` registered, so it can't be talked into serving the internet by a wrong setting.

## Stability

`Remote`, `RegisterRemote` and the `"remote"` field in `hello` stay as they are for every 1.x release.

