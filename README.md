# Media Core

A private Plex replacement. A server on the home box ("mint") downloads links straight into the media library, and the Android TV and phone apps browse and play that library.

| Part | Where | What |
|---|---|---|
| **media-core-engine** | `engine/` | Go server, standard library only, with its web UI embedded. Downloads through aria2, unpacks, renames, fixes Dolby Vision, keeps the catalog with TMDB metadata, records watch progress, finds intros, makes scrub thumbnails, streams files and serves WebDAV. |
| **Web player** | `engine/web` | The site at `mint:8780`: the TV app's screens and player in a browser (plain JS), and Manager: paste a link and see what it is before it downloads, the downloads, the library's files with where each plays, and mint's disk and health. Browsers play MP4s and Light copies directly ([docs/browser-playback.md](docs/browser-playback.md)). |
| **TV app** | `android/app` | Kotlin, Jetpack Compose and Media3. Id `app.mediacore`. Updates itself from the engine. |
| **Phone app** | `android/phone` | Its own screens and player. Id `app.mediacore.phone`. Has offline downloads. |
| **Shared core** | `android/core` | Used by both apps: the engine API, its data, offline copies and scrub pictures. |

```
engine/          Go module: *.go, web/ (embedded UI), testdata/
android/         Gradle project: app/ (TV), phone/, core/ (shared), brand/ (logo sources), build.sh
deploy/          install.sh, backup-state.sh, systemd user units, aria2.conf template
scripts/         Android SDK setup, headless emulators
docs/            design systems (TV, phone, web), browser-playback.md, PENDING.md (changes waiting for a build)
```

## How it runs

```
link ─▶ :8780 site + API (Tailscale only) ─▶ aria2 RPC 127.0.0.1:6800 ─▶ /srv/data/media
                                                                                     │
TV / phone app ◀─ :8790 player API /api/v1, streaming, WebDAV /dav (home LAN + Tailscale) ◀─ catalog
```

- The engine never transcodes while you watch. The apps get files as they are, with HTTP ranges, and decode them (Media3, plus FFmpeg decoders for DTS and TrueHD). Browsers get copies made ahead of time.
- Library layout: `Movies/<Title> (<Year>)/…`, `TV Shows/<Show>/Season 01/<Show> - S01E01.ext` and `Music/<Artist>/<Album>/`. The catalog skips dot-folders.
- Two systemd user services, `media-core-engine` and `media-core-aria2`, plus the nightly `media-core-backup.timer`.

| Path | What | In git? |
|---|---|---|
| `~/.config/media-core/` | `aria2.conf` (RPC secret), `tmdb.key`, `webdav.pass`, `app.jks` and `app.jks.pass` (the app signing key) | **never** |
| `~/.local/share/media-core/` | jobs, watch state, TMDB cache, thumbs, intros, built APKs | no (runtime data) |
| `~/.local/bin/media-core-engine` | engine binary, built by `deploy/install.sh` | no |

Every night `media-core-backup` saves watch progress, settings, intro data and title matches to `~/.local/share/media-core/backups/state/`, and to a USB stick mounted at `/mnt/media-core-usb` when there is one. It keeps 30 days. To restore, stop the engine, unpack the newest `state-*.tgz` into `~/.local/share/media-core/` and start it again. The signing key isn't in this backup, so keep a copy off the box.

**Keep `app.jks` and `app.jks.pass` safe.** Every build must be signed with them. If the key is lost, the installed apps won't take updates and have to be uninstalled, which wipes their offline downloads.

## Working on a new machine

```sh
git clone git@github.com:faizanxmd/media-core.git && cd media-core
mkdir -p ~/.config/media-core && chmod 700 ~/.config/media-core
scp 'mint:.config/media-core/{app.jks,app.jks.pass,tmdb.key}' ~/.config/media-core/
```

### Engine

You need Go 1.27 or newer.

```sh
cd engine
go vet ./... && go test ./...
```

To run it locally against a scratch library instead of `/srv/data/media`, you need aria2 and a config:

```sh
sed -e "s|@HOME@|$HOME|g" -e "s|@SECRET@|dev|" deploy/aria2.conf.in > ~/.config/media-core/aria2.conf
mkdir -p ~/.local/share/media-core && touch ~/.local/share/media-core/aria2.session
aria2c --conf-path="$HOME/.config/media-core/aria2.conf" &
cd engine && MEDIA_CORE_LIBRARY=~/media-test go run . serve   # UI on :8780, player API on :8790
```

ffmpeg, ffprobe, 7z, mkvmerge and `~/.local/bin/dovi_tool` are only needed for post-processing, thumbnails and media info. Those steps run under `ionice`, so they work on Linux only. On macOS, browsing and streaming still work.

### Android apps

You need JDK 17 and the Android SDK (platform 36, build-tools 36). Gradle comes from `android/gradlew`.

- **Linux:** run `sudo apt install openjdk-17-jdk-headless`, then `scripts/android-setup.sh`. The SDK goes to `~/android/sdk`.
- **macOS:** install Android Studio, or `brew install --cask android-commandlinetools`, and set `ANDROID_HOME`.
- Android Studio can open `android/` directly.

Builds normally run on GitHub Actions ([.github/workflows/build.yml](.github/workflows/build.yml)), started by hand, so mint only downloads the result:

```sh
scripts/ci-build.sh                 # build both apps from origin/main, wait, publish on mint
scripts/ci-build.sh tv              # or just one: tv | phone
scripts/ci-build.sh --latest        # publish the newest build started from the GitHub site or app
```

The workflow signs with the repo secrets `MEDIA_CORE_KEYSTORE_B64` (base64 of `app.jks`) and `MEDIA_CORE_KS_PASS`. GitHub can't give these back, so they aren't a backup of the key.

To build locally instead (slow and heavy on mint):

```sh
android/build.sh                    # TV app  -> ~/.local/share/media-core/app/media-core.apk (+ .json version file)
android/build.sh --phone            # phone   -> ~/.local/share/media-core/app/media-core-phone.apk (+ .json)
android/build.sh --install <tv-ip>  # TV app, then adb install (TV: Developer options > Network debugging)
```

The engine serves the APKs at `/api/v1/app.apk` (TV) and `/api/v1/phone-app.apk` (phone), and each app offers an update when a newer build appears. `MEDIA_CORE_APK_DIR` changes where the build output goes.

Headless emulators for UI checks: `scripts/android-setup.sh --emulator`, then `scripts/emulator.sh tv|phone`.

## Setting up the engine host

On a fresh Linux box (mint runs Linux Mint 22):

```sh
sudo apt install aria2 ffmpeg p7zip-full mkvtoolnix   # Ubuntu's ffmpeg has the chromaprint muxer Skip Intro uses
# Go: unpack the official tarball to ~/.local/go
# dovi_tool 2.3.x: ~/.local/bin/dovi_tool (github.com/quietvoid/dovi_tool releases)
# TMDB API key (v3): ~/.config/media-core/tmdb.key
sudo mkdir -p /srv/data/media/{Movies,"TV Shows",Music}   # owned by your user, group media, mode 2775
sudo loginctl enable-linger "$USER"                       # user services start at boot
deploy/install.sh
```

To also keep the nightly backup on a USB stick, so it survives the main disk dying, add it to `/etc/fstab` using its UUID and filesystem type from `lsblk -f`, then make the folder and run `sudo systemctl daemon-reload && sudo systemctl start mnt-media\\x2dcore\\x2dusb.automount`:

```
UUID=<uuid> /mnt/media-core-usb exfat noauto,nofail,x-systemd.automount,x-systemd.idle-timeout=60,x-systemd.device-timeout=5,uid=1000,gid=1000,umask=077 0 0
```

`deploy/install.sh` builds, tests and installs the binary and units, creates `aria2.conf` with a random secret the first time, and restarts the engine. Re-running it deploys engine changes. Firewall: allow 8780 on `tailscale0` only, and 8790 on the home LAN plus `tailscale0`.

## Sharing with a friend

mint is only reachable over Tailscale, so a friend joins through it, with no public link and no password:

1. In the Tailscale admin console (Machines), open `mint`'s menu, choose **Share…**, and send the invite to the friend's email.
2. The friend installs Tailscale, signs in with that email, and accepts.
3. They open `http://100.75.19.19:8780` in a browser (Chrome, Safari or Edge).

Anyone on another Tailscale account is a guest (`engine/guest.go`): they see Home, Films, Shows and Search and can play, with
their own watch progress (`~/.local/share/media-core/guests/`), but Manager, downloads, files and settings answer 403. Your own
devices and the home network are the owner, as before. Remove the share in the same menu to cut them off.

## Workflow

- `main` is the only branch. Each published app build is tagged, for example `tv-2026.10.30-2319` and `phone-2026.10.31-0019`.
- Changes that are committed but not yet built and tested are listed in [docs/PENDING.md](docs/PENDING.md). That list is cleared after each tested build.
- UI rules: [docs/design-system.md](docs/design-system.md) (TV) and [docs/design-system-phone.md](docs/design-system-phone.md).
- Engine API, briefly: `POST /api/probe {url}` (also reads the video details), `POST /api/jobs {probe,plan[,light]}`, `GET /api/manager/titles`, `POST /api/manager/mp4 {id}`, `POST /api/manager/light {id,probe}`, `GET /api/system`, `GET /api/jobs`, `POST /api/quick {url[,kind,title]}`, live events at `/api/events`. CLI: `media-core-engine add <url> [movie|tv|music] [title]` and `media-core-engine ls`.
