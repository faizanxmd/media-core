# Media Core

Media Core is a private replacement for Plex. A server on the home box, mint, downloads links straight into the media library. An Android TV app, a phone app and a website browse that library and play it.

## The parts

**media-core-engine** (`engine/`) is the server. It is written in Go with only the standard library, and the website is built into the binary. It downloads through aria2, unpacks and renames what arrives, fixes Dolby Vision, and keeps the catalog with metadata from TMDB. It also records watch progress, finds intros, makes scrub thumbnails, streams files and serves WebDAV.

**The website** (`engine/web`) lives at `mint:8780` and is plain JavaScript. It has the TV app's screens and player, plus Manager. In Manager you paste a link and see what it is before it downloads. Manager also shows the downloads, the library's files with where each one plays, and mint's disk and health.

**The TV app** (`android/app`) is Kotlin with Jetpack Compose and Media3. Its id is `app.mediacore`, and it updates itself from the engine.

**The phone app** (`android/phone`) has its own screens and player, and can keep downloads for offline. Its id is `app.mediacore.phone`.

**The shared core** (`android/core`) is what both apps use to talk to the engine: the API, its data, offline copies and scrub pictures.

The rest of the repo:

```
deploy/    install.sh, backup-state.sh, the systemd user units, the aria2.conf template
scripts/   Android SDK setup, headless emulators, the CI build, a browser driver for the site
docs/      design systems, browser-playback.md, PENDING.md, ISSUES.md
android/   also holds brand/ (logo sources) and build.sh
```

## How it runs

```
link ─▶ :8780 site + API (Tailscale only) ─▶ aria2 RPC 127.0.0.1:6800 ─▶ /srv/data/media
                                                                                     │
TV / phone app ◀─ :8790 player API /api/v1, streaming, WebDAV /dav (home LAN + Tailscale) ◀─ catalog
```

The engine never converts the picture while you watch. The apps get each file as it is, over HTTP ranges, and decode it themselves with Media3, plus FFmpeg decoders for DTS and TrueHD.

Browsers are the exception, because some can't open a file as it is: an MKV in Safari, or Dolby and DTS sound in Chrome. Those get the file repackaged while it plays. The picture is copied untouched, and the sound becomes AAC only where it has to. [docs/browser-playback.md](docs/browser-playback.md) has the details.

The library is laid out like this, and the catalog skips any folder whose name starts with a dot:

```
Movies/<Title> (<Year>)/…
TV Shows/<Show>/Season 01/<Show> - S01E01.ext
Music/<Artist>/<Album>/
```

Two systemd user services do the work, `media-core-engine` and `media-core-aria2`. A third unit, `media-core-backup.timer`, runs the nightly backup.

### Where things live on mint

None of this is in git.

- `~/.config/media-core/` holds the secrets: `aria2.conf` (with the RPC secret), `tmdb.key`, `webdav.pass`, and the app signing key, `app.jks` with `app.jks.pass`.
- `~/.local/share/media-core/` holds the runtime data: jobs, watch state, the TMDB cache, thumbnails, intros and the built APKs.
- `~/.local/bin/media-core-engine` is the engine binary, built by `deploy/install.sh`.

### The signing key

**Keep `app.jks` and `app.jks.pass` safe, with a copy off the box.** Every build must be signed with them. If the key is lost, the installed apps won't take updates and have to be uninstalled, which wipes their offline downloads. The nightly backup does not include the key.

### Backups

Every night `media-core-backup` saves watch progress, settings, intro data and title matches to `~/.local/share/media-core/backups/state/`. If a USB stick is mounted at `/mnt/media-core-usb`, it saves a copy there too. It keeps 30 days.

To restore, stop the engine, unpack the newest `state-*.tgz` into `~/.local/share/media-core/` and start the engine again.

## Working on a new machine

Clone the repo and copy the keys over from mint:

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

To run it against a scratch library instead of `/srv/data/media`, you also need aria2 and a config for it:

```sh
sed -e "s|@HOME@|$HOME|g" -e "s|@SECRET@|dev|" deploy/aria2.conf.in > ~/.config/media-core/aria2.conf
mkdir -p ~/.local/share/media-core && touch ~/.local/share/media-core/aria2.session
aria2c --conf-path="$HOME/.config/media-core/aria2.conf" &
cd engine && MEDIA_CORE_LIBRARY=~/media-test go run . serve   # site on :8780, player API on :8790
```

ffmpeg, ffprobe, 7z, mkvmerge and `~/.local/bin/dovi_tool` are only needed for post-processing, thumbnails and media info. Those steps run under `ionice`, so they work on Linux only. On macOS you can still browse and stream.

### Android apps

You need JDK 17 and the Android SDK (platform 36, build-tools 36). Gradle comes from `android/gradlew`, and Android Studio can open `android/` directly.

On Linux, run `sudo apt install openjdk-17-jdk-headless` and then `scripts/android-setup.sh`, which puts the SDK in `~/android/sdk`. On macOS, install Android Studio, or run `brew install --cask android-commandlinetools` and set `ANDROID_HOME`.

Builds normally run on GitHub Actions ([.github/workflows/build.yml](.github/workflows/build.yml)) and are started by hand, so mint only has to download the result:

```sh
scripts/ci-build.sh                 # build both apps from origin/main, wait, publish on mint
scripts/ci-build.sh tv              # or just one: tv | phone
scripts/ci-build.sh --latest        # publish the newest build started from the GitHub site or app
```

The workflow signs with two repo secrets, `MEDIA_CORE_KEYSTORE_B64` (base64 of `app.jks`) and `MEDIA_CORE_KS_PASS`. GitHub never gives secrets back, so they are not a backup of the key.

You can also build on mint, though it is slow and heavy there:

```sh
android/build.sh                    # TV app  -> ~/.local/share/media-core/app/media-core.apk (+ .json version file)
android/build.sh --phone            # phone   -> ~/.local/share/media-core/app/media-core-phone.apk (+ .json)
android/build.sh --install <tv-ip>  # TV app, then adb install (TV: Developer options > Network debugging)
```

Set `MEDIA_CORE_APK_DIR` to send the build output somewhere else.

The engine serves the APKs at `/api/v1/app.apk` (TV) and `/api/v1/phone-app.apk` (phone). Each app offers an update when a newer build appears there.

For UI checks without a device, `scripts/android-setup.sh --emulator` installs headless emulators and `scripts/emulator.sh tv|phone` starts one.

## Setting up the engine host

This is for a fresh Linux box. mint runs Linux Mint 22.

```sh
sudo apt install aria2 ffmpeg p7zip-full mkvtoolnix   # Ubuntu's ffmpeg has the chromaprint muxer Skip Intro uses
# Go: unpack the official tarball to ~/.local/go
# dovi_tool 2.3.x: ~/.local/bin/dovi_tool (github.com/quietvoid/dovi_tool releases)
# TMDB API key (v3): ~/.config/media-core/tmdb.key
sudo mkdir -p /srv/data/media/{Movies,"TV Shows",Music}   # owned by your user, group media, mode 2775
sudo loginctl enable-linger "$USER"                       # user services start at boot
deploy/install.sh
```

`deploy/install.sh` builds and tests the engine, installs the binary and the units, and restarts the engine. The first time, it also creates `aria2.conf` with a random secret. Run it again whenever you want engine changes to go live.

### Firewall

Allow port 8780 on `tailscale0` only. Allow 8790 on the home LAN and on `tailscale0`.

On Tailscale, 8780 answers only the owner's own devices (`engine/owner.go`). Sharing mint with another Tailscale account does not open Manager to it.

### Backup to a USB stick

A USB stick keeps the nightly backup safe if the main disk dies. Find the stick's UUID and filesystem type with `lsblk -f`, then add a line like this to `/etc/fstab`:

```
UUID=<uuid> /mnt/media-core-usb exfat noauto,nofail,x-systemd.automount,x-systemd.idle-timeout=60,x-systemd.device-timeout=5,uid=1000,gid=1000,umask=077 0 0
```

Make the folder, then turn the mount on:

```sh
sudo mkdir -p /mnt/media-core-usb
sudo systemctl daemon-reload && sudo systemctl start mnt-media\\x2dcore\\x2dusb.automount
```

## Workflow

`main` is the only branch. Each published app build gets a tag named after its version, such as `tv-2026.10.30-2319` or `phone-2026.10.31-0019`.

Changes that are committed but not yet built and tested are listed in [docs/PENDING.md](docs/PENDING.md). The list is cleared after each tested build.

The UI rules are in [docs/design-system.md](docs/design-system.md) for the TV and [docs/design-system-phone.md](docs/design-system-phone.md) for the phone.

## Engine API

Adding a download takes two calls. `POST /api/probe {url}` says what a link is, including the video details, and returns a probe and a plan. `POST /api/jobs {probe,plan[,light]}` starts the download. `POST /api/quick {url[,kind,title]}` does both in one step.

`GET /api/jobs` lists the downloads, `GET /api/system` reports mint's disk and health, and `/api/events` sends live events.

Manager uses `GET /api/manager/titles`, `POST /api/manager/mp4 {id}` and `POST /api/manager/light {id,probe}`.

There is a small command line too:

```sh
media-core-engine add <url> [movie|tv|music] [title]
media-core-engine ls
```
