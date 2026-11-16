# How Media Core works

## The parts

**media-core-engine** (`engine/`) is the server. It is written in Go with only the standard library, and the website is built into the binary. It downloads through aria2, unpacks and renames what arrives, fixes Dolby Vision, and keeps the catalog with metadata from TMDB. It also records watch progress, finds intros, makes scrub thumbnails, streams files and serves WebDAV.

**The website** (`engine/web`) is plain JavaScript on port 8780. It has the TV app's screens and player, plus Manager. In Manager you paste a link and see what it is before it downloads. Manager also shows the downloads, the library's files with where each one plays, and the server's disk and health.

**The TV app** (`android/app`) is Kotlin with Jetpack Compose and Media3. Its id is `app.mediacore`, and it updates itself from the engine.

**The phone app** (`android/phone`) has its own screens and player, and can keep downloads for offline. Its id is `app.mediacore.phone`.

**The shared core** (`android/core`) is what both apps use to talk to the engine: the API, its data, offline copies and scrub pictures.

## Ports and flow

```
link ─▶ :8780 site + API (Tailscale only) ─▶ aria2 RPC 127.0.0.1:6800 ─▶ library
                                                                            │
TV / phone app ◀─ :8790 player API /api/v1, streaming, WebDAV /dav ◀─ catalog
                  (home network + Tailscale; UDP 8790 answers the apps' search)
```

The engine never converts the picture while you watch. The apps get each file as it is, reading the parts they need (HTTP range requests), and decode it themselves with Media3, plus FFmpeg decoders for DTS and TrueHD.

Browsers are the exception, because some can't open a file as it is: an MKV in Safari, or Dolby and DTS sound in Chrome. Those get the file repackaged while it plays: same picture, a different container. The picture is copied untouched, and the sound becomes AAC only where it has to. [browser-playback.md](browser-playback.md) has the details.

## How the apps find the server

An app races every address it knows and prefers the home network. It learns them from the server: `GET /api/v1/hello` lists the server's name, its home-network and Tailscale addresses, and its Tailscale name. A fresh app finds the server by sending `media-core?` as a UDP broadcast to port 8790; the engine answers devices on the home network with the same list. Away from home, the download page at `http://<server>:8790/` has an "Open in Media Core" button, a `mediacore://connect?u=…` link that hands the app the addresses. If neither works, the app's offline screen takes a typed address.

## Who can use it

There are no accounts. The engine answers the home network, and of Tailscale only the owner's own devices (`engine/owner.go`), so sharing the server with another Tailscale account does not open it to them. The firewall ([setup.md](setup.md), step 6) keeps 8780 to Tailscale. Anyone on the home network can use the apps' API, so never expose either port to the internet.

## The library

The catalog skips any folder whose name starts with a dot.

```
Movies/<Title> (<Year>)/…
TV Shows/<Show>/Season 01/<Show> - S01E01.ext
Music/<Artist>/<Album>/
```

## Engine API

The main routes. Adding a download takes two calls. `POST /api/probe {url}` says what a link is, including the video details, and returns a probe and a plan. `POST /api/jobs {probe,plan[,light]}` starts the download. `POST /api/quick {url[,kind,title]}` does both in one step.

`GET /api/jobs` lists the downloads, `GET /api/system` reports the server's disk and health, and `/api/events` sends live events. `GET /api/library` lists the library's files.

Manager uses `GET /api/manager/titles`, `POST /api/manager/light {id,probe}` and `POST /api/manager/light/delete`.

The apps use `/api/v1/…` on port 8790: `hello`, `home`, `movies`, `shows`, `search`, `item/<id>`, `progress`, `watched`, `stream/<id>`, `sub/<id>/<n>`, `thumbs/<id>`, `img/…`, `speedtest`, and the APKs at `app.apk` and `phone-app.apk` (their versions at `app` and `phone-app`). The website also uses `hls/…` and `web/…` for repackaged playback.

There is a small command line too:

```sh
media-core-engine add <url> [movie|tv|music] [title]
media-core-engine ls
```
