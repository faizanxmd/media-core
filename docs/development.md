# Development

```sh
git clone https://github.com/faizanxmd/media-core.git && cd media-core
git config core.hooksPath .githooks   # blocks private details in commits (scripts/check-private.sh)
```

## Engine

You need Go 1.27 or newer.

```sh
cd engine
go vet ./... && go test ./...
```

Tests never touch real state: `engine/main_test.go` points every state path at a temporary folder.

To try it against a folder of videos, run a temporary server. It needs no install and no settings, runs its own aria2 if aria2 is installed, and with no `--state` keeps everything in a temp folder that it removes when you press Ctrl-C:

```sh
cd engine && go run . serve --library ~/Videos --port 28780   # site on :28780, apps' API on :28790
```

Leave out `--port` on a machine with no installed server. A TMDB key the installed server already has is reused.

To install the engine from this tree as the real service (tests, builds, keeps the old binary as `.prev`, then runs `media-core-engine install --yes`):

```sh
deploy/install.sh
```

`scripts/test-install.sh` runs the one-line install end to end in a throwaway home, without GitHub or systemd.

ffmpeg, ffprobe, 7z, mkvmerge and dovi_tool are only needed for post-processing, thumbnails and media info. They run at low priority (`ionice` where there is one, else `nice`). The engine builds and runs on Linux and macOS.

`scripts/web-drive.py` drives the site in headless Firefox, to check the web player and Manager after a change.

## Android apps

You need JDK 17 and the Android SDK (platform 36, build-tools 36). Gradle comes from `android/gradlew`, and Android Studio can open `android/` directly.

On Linux, run `sudo apt install openjdk-17-jdk-headless` and then `scripts/android-setup.sh`, which puts the SDK in `~/android/sdk`. On macOS, install Android Studio, or run `brew install --cask android-commandlinetools` and set `ANDROID_HOME`. For UI checks without a device, `scripts/android-setup.sh --emulator` installs headless emulators and `scripts/emulator.sh tv|phone` starts one.

Builds run on GitHub Actions ([build.yml](../.github/workflows/build.yml)), started by hand, so the server only downloads the result. `scripts/ci-build.sh` needs the `gh` command, signed in, and your local HEAD equal to `origin/main`:

```sh
scripts/ci-build.sh                 # build both apps from origin/main, wait, publish where the engine serves them
scripts/ci-build.sh tv              # or just one: tv | phone
scripts/ci-build.sh --latest        # publish the newest build started from the GitHub site or app
```

The workflow signs with two secrets in the repo's `release` environment, `MEDIA_CORE_KEYSTORE_B64` (base64 of `app.jks`) and `MEDIA_CORE_KS_PASS`. Limit that environment to `v*` tags and `main`, with the owner as required reviewer, so a pull request or another branch can't reach the key and every signed build waits for one approval. GitHub never gives secrets back, so they are not a backup of the key: keep `app.jks` and its password somewhere safe. Every build prints the certificate's fingerprint (`apksigner verify --print-certs`) in its log.

App versions follow the release tag: `vX.Y.Z` gives the name `X.Y.Z` and the code `X*10000000 + Y*100000 + Z*1000`. A build between releases adds the number of commits since the tag to the code (at most 999) and `-dev.N` to the name.

You can also build locally, though it is slow on a small machine:

```sh
android/build.sh                    # TV app  -> ~/.local/share/media-core/app/media-core.apk (+ .json version file)
android/build.sh --phone            # phone   -> ~/.local/share/media-core/app/media-core-phone.apk (+ .json)
android/build.sh --install <tv-ip>  # TV app, then adb install (TV: Developer options > Network debugging)
```

Set `MEDIA_CORE_APK_DIR` to send the build output somewhere else. The engine serves the APKs at `/api/v1/app.apk` and `/api/v1/phone-app.apk`, and each app offers an update when a newer build appears there.

## Releases

Tag `vX.Y.Z` on `main` and push the tag. [release.yml](../.github/workflows/release.yml) tests, builds the server for Linux and macOS (amd64, arm64), builds and signs both apps (through build.yml, so it waits for the `release` environment's approval), and publishes everything with one `SHA256SUMS`, build attestations and the notes from `CHANGELOG.md`. The tag needs a `## [X.Y.Z]` heading in `CHANGELOG.md` or the release stops before building. The release is made as a draft and published only when every file is there. A tag with a suffix (`v1.1.0-rc.1`) makes a pre-release.

`scripts/ci-build.sh` is for trying app builds on your own server between releases.

## Checks

The [Check workflow](../.github/workflows/check.yml) runs on every push: gofmt, vet (also for macOS and 32-bit ARM) and the engine tests with the race detector; actionlint on the workflows and a syntax check of `install.sh` under dash and busybox; a compile of both apps; shellcheck and the one-line install on Debian, Fedora and Arch; `scripts/check-private.sh --all` with a denylist from the `MEDIA_CORE_DENYLIST` secret; and gitleaks over the whole history.

## Design

The UI principles for TV, phone and web are in [design.md](design.md) (exact tokens live in `Design.kt`, `Theme.kt` and `app.css`). [browser-playback.md](browser-playback.md) explains how the web player picks what to play.
