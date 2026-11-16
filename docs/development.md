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

To run it against a scratch library, with its own settings and state, you also need aria2. Never do this on the box that runs the live engine: the ports clash.

```sh
cd ..   # back to the repo root
export MEDIA_CORE_CONFIG_DIR=~/media-test/conf MEDIA_CORE_DATA_DIR=~/media-test/data MEDIA_CORE_LIBRARY=~/media-test/library
mkdir -p "$MEDIA_CORE_CONFIG_DIR" "$MEDIA_CORE_DATA_DIR" "$MEDIA_CORE_LIBRARY"
sed -e "s|@HOME@/.local/share/media-core|$MEDIA_CORE_DATA_DIR|g" -e "s|@SECRET@|dev|" deploy/aria2.conf.in > "$MEDIA_CORE_CONFIG_DIR/aria2.conf"
touch "$MEDIA_CORE_DATA_DIR/aria2.session"
aria2c --conf-path="$MEDIA_CORE_CONFIG_DIR/aria2.conf" &
cd engine && go run . serve   # site on :8780, apps' API on :8790
```

ffmpeg, ffprobe, 7z, mkvmerge and dovi_tool are only needed for post-processing, thumbnails and media info. Those steps run under `ionice`, so they work on Linux only. On macOS you can still browse and stream.

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

The workflow signs with two repo secrets, `MEDIA_CORE_KEYSTORE_B64` (base64 of `app.jks`) and `MEDIA_CORE_KS_PASS`. GitHub never gives secrets back, so they are not a backup of the key. The repo variable `MEDIA_CORE_TZ` sets the time zone of the version names (UTC if unset).

You can also build locally, though it is slow on a small machine:

```sh
android/build.sh                    # TV app  -> ~/.local/share/media-core/app/media-core.apk (+ .json version file)
android/build.sh --phone            # phone   -> ~/.local/share/media-core/app/media-core-phone.apk (+ .json)
android/build.sh --install <tv-ip>  # TV app, then adb install (TV: Developer options > Network debugging)
```

Set `MEDIA_CORE_APK_DIR` to send the build output somewhere else. The engine serves the APKs at `/api/v1/app.apk` and `/api/v1/phone-app.apk`, and each app offers an update when a newer build appears there.

## Versions

Each published app build gets a tag named after its version, such as `tv-2026.11.16-0031` or `phone-2026.10.31-0019`.

## Checks

The [Check workflow](../.github/workflows/check.yml) runs on every push: gofmt, vet and the engine tests, `scripts/check-private.sh --all` with a denylist from the `MEDIA_CORE_DENYLIST` secret, and gitleaks over the whole history.

## Design

The UI rules are in [design-system.md](design-system.md) for the TV and [design-system-phone.md](design-system-phone.md) for the phone. The site follows the TV's rules; [browser-playback.md](browser-playback.md) explains how the web player picks what to play.
