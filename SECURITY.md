# Security

## Reporting a problem

Please report security problems privately, through GitHub's [private vulnerability reporting](https://github.com/faizanxmd/media-core/security/advisories/new), not in a public issue. You'll get an answer within a week. Fixes go into a release, and the advisory says which versions are affected.

## How Media Core protects a server

Media Core is made for one home: a server on the home network, and the TVs, phones and browsers in that home. Two checks guard every request.

**1. The network.** The server answers only its own computer, private home-network addresses (10/8, 172.16/12, 192.168/16, IPv6 unique-local and link-local) and nothing else, whatever the firewall or router allows. A port forwarded to the internet by mistake still gets "403: only open to its home network". It also refuses requests sent to a host name that isn't a home-network name, so a web page on the internet can't use DNS rebinding to reach the server through someone's browser. Names it takes: IP addresses, one-word names, names ending in `.local`, `.lan`, `.home`, `.home.arpa` or `.internal`, and any listed in `MEDIA_CORE_HOSTS`.

**2. A device key.** Every device has its own key, and every request needs one, apart from the few that find the server, hand out the apps, set it up or pair a device.

- A key is 32 random bytes. The server keeps only its SHA-256, in `~/.config/media-core/devices.json` (readable only by the server's user).
- Apps send it as `Authorization: Bearer`. Browsers keep it in an HttpOnly, SameSite=Strict cookie that page scripts can't read.
- A device gets its key by **pairing**: it shows a 6-letter code for 10 minutes, someone on an admin device approves it, and only the device that asked (which holds a secret of its own) can collect the key. Seeing the code isn't enough.
- The **first admin** comes from a setup code the server prints when it's installed. It works once, lasts 24 hours, and locks for 15 minutes after 10 wrong tries.
- Two roles. A **player** can watch (the apps' API). An **admin** can do everything: downloads, deleting files, settings and devices. The server's own command line has an admin key in `cli.token`.
- An app sends its key only to a server that first proves it knows that key (an HMAC of a random challenge, keyed by the key's hash). Another box that answers on the same address, say on a different Wi-Fi, never sees it.
- Removing a device on the website ends its key on its next request.

The downloader (aria2) listens on localhost only, with a random password. WebDAV (`/dav`, read-only) has its own password in `webdav.pass`.

## What it doesn't protect against

- **Someone on the home network watching the traffic.** The server speaks plain HTTP, because a home server has no certificate a TV would trust. Someone who can read the home network's traffic can read keys and what's being watched. Use a trusted network (WPA2/WPA3 Wi-Fi, no strangers on it).
- **Other web services on the same computer.** Browsers send cookies to every port of a host name, so a different web service on the server's own name and address would receive the browser's key. Don't run untrusted web services on the same computer under the same name.
- **Someone with an account on the server.** The settings and keys are protected by file permissions, not encryption.
- **Use away from home.** It's not supported yet. Don't put the server on the internet. A future add-on will plug in through a single hook ([docs/remote.md](docs/remote.md)) with its own protection.

## Supported versions

Only the latest release gets security fixes. `media-core-engine upgrade` installs it.
