# Media Core

See [README.md](README.md) for the layout, setup and how the parts fit.

## Builds
Build the apps only when the user asks. Never run `scripts/ci-build.sh`, `android/build.sh`, Gradle build tasks or adb installs on your own, and don't offer a build after each change.

Builds run on GitHub Actions, not on mint: `scripts/ci-build.sh [both|tv|phone]` starts one, waits and publishes the APKs on mint, where both apps offer the update. Run it as its own systemd unit. Keep `android/build.sh` for when GitHub can't be used.

Make the change, commit it to GitHub, and add it to [docs/PENDING.md](docs/PENDING.md). When the batch is ready, the user asks for the build, tests it and reports back. After that build, clear the list and tag the commit (`tv-YYYY.MM.DD-HHMM` / `phone-...`, the build's version name).

Engine changes go live with `deploy/install.sh` (builds, tests, restarts the service on mint).

## Two apps, one copy each
`android/app` (TV) and `android/phone` share no code. A fix in the copied plumbing (Api, Models, Offline, Thumbs, player engine) goes in both.

## Watch state
The engine's `engine/watch.go` is the one record of progress, and every app reads it. An item is not started, part-way or finished, never two at once. Rules about progress belong there, not in an app.
