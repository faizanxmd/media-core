# Media Core

## Builds
Build the apps only when the user asks. Never run `android/build.sh`, Gradle build tasks or adb installs on your own, and don't offer a build after each change.

Make the change, commit it to GitHub, and add it to [docs/PENDING.md](docs/PENDING.md). When the batch is ready, the user asks for the build, tests it and reports back. After that build, clear the list.

## Two apps, one copy each
`android/app` (TV) and `android/phone` share no code. A fix in the copied plumbing (Api, Models, Offline, Thumbs, player engine) goes in both.

## Watch state
The engine's `watch.go` is the one record of progress, and every app reads it. An item is not started, part-way or finished, never two at once. Rules about progress belong there, not in an app.
