# Security

## Reporting a problem

Please report security problems privately, through GitHub's [private vulnerability reporting](https://github.com/faizanxmd/media-core/security/advisories/new), not in a public issue. Say which version you run, what an attacker needs (on the home network? a paired device?) and how to reproduce it. You'll get an answer within seven days. Fixes go into a release, and the advisory says which versions are affected.

## How Media Core protects a server

Media Core is made for one home: a server on the home network, and the TVs, phones and browsers in that home. Two checks guard every request.

**1. The network.** The server answers only its own computer, private home-network addresses (10/8, 172.16/12, 192.168/16, IPv6 unique-local and link-local) and nothing else (Tailscale's 100.64/10 range included, on purpose), whatever the firewall or router allows. A port forwarded to the internet by mistake still gets "403: only open to its home network". It also refuses requests sent to a host name that isn't a home-network name, so a web page on the internet can't use DNS rebinding to reach the server through someone's browser. Names it takes: IP addresses, one-word names, names ending in `.local`, `.lan`, `.home`, `.home.arpa` or `.internal`, and any listed in `MEDIA_CORE_HOSTS`.

**2. A device key.** Every device has its own key, and every request needs one, apart from the few that find the server, hand out the apps, set it up or pair a device.

- A key is 32 random bytes. The server keeps only its SHA-256, in `~/.config/media-core/devices.json` (readable only by the server's user).
- Apps send it as `Authorization: Bearer`. Browsers keep it in an HttpOnly, SameSite=Strict cookie that page scripts can't read.
- A device gets its key by **pairing**: it shows a 6-letter code for 10 minutes, someone on an admin device approves it, and only the device that asked (which holds a secret of its own) can collect the key. Seeing the code isn't enough.
- The **first admin** comes from a setup code the server prints when it's installed. It works once and lasts 24 hours. Ten wrong tries from one address lock that address out for 15 minutes, and a hundred from everywhere together lock setup for everyone.
- Two roles. A **player** can watch (the apps' API). An **admin** can do everything: downloads, deleting files, settings and devices. The server's own command line has an admin key in `cli.token`.
- An app sends its key only to a server that first proves it knows that key (an HMAC of a random challenge, keyed by the key's hash). Another box that answers on the same address, say on a different Wi-Fi, never sees it.
- Removing a device ends its key at once, including streams it has running.
- Requests that change something must be JSON (`Content-Type: application/json`), and the server answers no CORS preflight, so another website can't drive it through a visitor's browser.

The downloader (aria2) listens on localhost only, with a random password.

## What it doesn't protect against

- **Someone who can watch the home network's traffic.** Everything travels over plain HTTP, because a home server has no certificate a TV would trust. Someone who can read the traffic (a compromised router, a device sniffing open Wi-Fi) can see what's watched and copy a device key or a browser cookie and use it. The key proof stops an app sending its key to a fake server; it doesn't hide the traffic. Use a trusted network (WPA2/WPA3, no strangers on it).
- **WebDAV.** `/dav` (read-only) uses HTTP Basic with one shared password from `webdav.pass`, sent with every request.
- **What's open without a key**, to anyone on the home network: `hello` (the server's name, version and addresses), the apps' download page and the APKs, the site's static files, whether the server is set up, and starting and polling a pairing.
- **Per-person libraries.** Every paired device can play everything in the library. There are no separate users.
- **Its own user.** The server runs as your user and can write wherever that user can. The settings and keys are protected by file permissions (0600), not encryption: anyone who can read that user's files can read `cli.token` and is an admin.
- **Hostile media files.** ffmpeg, aria2 and 7-Zip aren't sandboxed. A booby-trapped file or archive is handled by them as your user.
- **Other web services on the same computer.** Browsers send cookies to every port of a host name, so another web service on the server's own name and address would receive the browser's key. Don't run untrusted web services on the same computer under the same name.
- **Use away from home.** Not in version 1. A future add-on will plug in through a single hook ([docs/remote.md](docs/remote.md)) with its own protection.

## Don't

- Forward ports 8780 or 8790 on the router. The server refuses outside addresses anyway, but don't rely on one check.
- Put a reverse proxy in front of it without understanding the address and host checks: a proxy on the same computer makes every request look local.
- Run it as root. `install` refuses to.

## Checking a download

Every release has a `SHA256SUMS` covering all its files (the one-line install and `upgrade` check it), and a build attestation that proves GitHub Actions built each file from this repo:

```sh
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify media-core-engine-linux-amd64 --repo faizanxmd/media-core
```

The apps are signed with one key for every release. Its certificate's SHA-256 fingerprint is printed in the log of every release build (`apksigner verify --print-certs`); Android refuses an update signed with any other key.

## Supported versions

Only the latest release gets security fixes. `media-core-engine upgrade` installs it.
