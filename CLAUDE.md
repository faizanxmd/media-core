# Media Core

See [README.md](README.md) for the layout, setup and how the parts fit. If a `CLAUDE.local.md` sits next to this file, read it too: it holds the rules for the machine you're on.

## Builds
Build the apps only when asked. Never run `scripts/ci-build.sh`, `android/build.sh`, Gradle build tasks or adb installs on your own, and don't offer a build after each change.

Builds run on GitHub Actions: `scripts/ci-build.sh [both|tv|phone]` starts one, waits, and publishes the APKs where the engine serves them, so both apps offer the update. Run it as its own systemd unit. Keep `android/build.sh` for when GitHub can't be used.

Make the change, commit it, and list it in `notes/PENDING.md` (personal notes, kept out of git). When a batch is ready, the owner asks for the build, tests it and reports back. After that build, clear the list. Releases are tagged `vX.Y.Z` (the release workflow builds the apps, so a tag needs the owner's OK); app versions come from that tag.

Engine changes go live with `deploy/install.sh`, which builds, tests and restarts the service. Check that nothing is playing or downloading first.

## Two apps, one shared core
`android/core` holds what both apps use to talk to the engine: Api, Models, Offline and Thumbs. Fix those once, there. Screens stay in `android/app` (TV) and `android/phone`, and each keeps its own player, so a fix in the player engine goes in both.

## Watch state
The engine's `engine/watch.go` is the one record of progress, and every app reads it. An item is not started, part-way or finished, never two at once. Rules about progress belong there, not in an app.

## Tests
Engine tests never touch real state: `engine/main_test.go` points every state path at a temporary folder. Keep it, and use `t.TempDir()` in new tests.

## Nothing private in git
Run `git config core.hooksPath .githooks` once per clone. The pre-commit hook runs `scripts/check-private.sh`, which blocks tailnet names, home and Tailscale addresses, home paths, emails and keys, plus the strings in `~/.config/media-core/private-denylist`. Settings for one machine go in `~/.config/media-core/engine.env`, never in code or docs. Personal notes go in `notes/`, which git ignores.
