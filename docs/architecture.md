# How Media Core works

## The parts

**media-core-engine** (`engine/`) is the server. It is written in Go with only the standard library, and the website is built into the binary. It downloads through aria2, unpacks and renames what arrives, fixes Dolby Vision, and keeps the catalog with metadata from TMDB. It also records watch progress, finds intros, makes scrub thumbnails, streams files and serves WebDAV.

**The website** (`engine/web`) is plain JavaScript on port 8780. It has the TV app's screens and player, plus Manager. In Manager you paste a link and see what it is before it downloads. Manager also shows the downloads, the library's files with where each one plays, and the server's disk and health.

**The TV app** (`android/app`) is Kotlin with Jetpack Compose and Media3. Its id is `app.mediacore`, and it updates itself from the engine.

**The phone app** (`android/phone`) has its own screens and player, and can keep downloads for offline. Its id is `app.mediacore.phone`.

**The shared core** (`android/core`) is what both apps use to talk to the engine: the API, its data, offline copies and scrub pictures.

## Ports and flow

```
link ─▶ :8780 site + API (home network) ─▶ aria2 RPC 127.0.0.1:6800 ─▶ library
                                                                            │
TV / phone app ◀─ :8790 player API /api/v1, streaming, WebDAV /dav ◀─ catalog
                  (home network; UDP 8790 answers the apps' search)
```

The engine never converts the picture while you watch. The apps get each file as it is, reading the parts they need (HTTP range requests), and decode it themselves with Media3, plus FFmpeg decoders for DTS and TrueHD.

Browsers are the exception, because some can't open a file as it is: an MKV in Safari, or Dolby and DTS sound in Chrome. Those get the file repackaged while it plays: same picture, a different container. The picture is copied untouched, and the sound becomes AAC only where it has to. [browser-playback.md](browser-playback.md) has the details.

## How the apps find the server

An app races every address it knows. It learns them from the server: `GET /api/v1/hello` lists the server's name and its home-network addresses. A fresh app finds the server by sending `media-core?` as a UDP broadcast to port 8790; the engine answers devices on the home network with the same list. The download page at `http://<server>:8790/` also has an "Open in Media Core" button, a `mediacore://connect?u=…` link that hands the app the addresses. If neither works, the app's offline screen takes a typed address. Use away from home isn't supported yet.

## Who can use it

Two checks guard every request. The first is the network: the engine answers only this machine and the home network (`engine/gate.go`), whatever the firewall allows, and only when the request names the server by an address or a home-network name. The second is a device key (`engine/auth.go`): every TV, phone and browser is paired once with a code, and an admin device approves it (`engine/pair.go`). Keys are stored only as hashes; roles are player (watch) and admin (everything). [SECURITY.md](../SECURITY.md) has the whole model.

Reaching the server away from home is a hook for a later add-on: `RegisterRemote` in `gate.go` ([remote.md](remote.md)).

## The library

The catalog skips any folder whose name starts with a dot.

```
Movies/<Title> (<Year>)/…
TV Shows/<Show>/Season 01/<Show> - S01E01.ext
Music/<Artist>/<Album>/
```

## Engine API

[api.md](api.md) lists the routes. The command line (`media-core-engine help`) covers installing, upgrading, devices, downloads and backups.
