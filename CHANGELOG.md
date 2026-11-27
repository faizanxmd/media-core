# Changelog

What changed for people using Media Core, newest first. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/): the server, the apps and the API version (`"api"` in `/api/v1/hello`) move together in each release.

## [Unreleased]

### Fixed

- TV app and Phone app: coalesce rapid 10-second skips into a single seek (350 ms debounce) so multi-tap skips don't flood hardware video decoders on high-bitrate 4K/Dolby Vision files; trim back-buffer to 12 s and size the video buffer to 25% of `largeHeap` (64–192 MB).
- Engine: fix zombie `ffmpeg` processes on stale HLS runs, PID-reuse signal races in `SIGSTOP`/`SIGCONT` handling, `Store` slice data races, duplicate `ffprobe` keyframe scans, unbounded init-header retries, and orphaned stream processes on shutdown; free idle HLS piece caches after 2 min, cut MP4 box double-allocations in `stream.go`, cache `os.ReadDir` in catalog scans, prune stale thumbnails when idle, and support symlinked media roots.
- Site / Safari: fix Safari `audioTracks` startup race and missing-track silence, double-click fullscreen while controls are visible, `pause`/`ended` watch-progress race, uncancelled recovery/HLS retry timers, `playNext` post-close race, PGS buffer leaks and z-order drawing, Manager polling leak, and missing Settings icon; lazy-load `hls.js` so Safari skips 542 KB of render-blocking JS, and pause the subtitle loop when idle.
- TV app and shared core: prevent `DefaultLoadControl` buffering deadlock on high-bitrate 4K files by prioritizing time over size thresholds; fix skip-coalescing edge cases, media Play/Pause key handling while buffering, D-pad `FocusRequester` crashes, `ThumbStore` thread/FD/0-thumb bugs, per-origin proof locking, cold-start duplicate home fetch, and Settings speedtest error handling; move shadow baking to Skia `BlurMaskFilter`, remove boxing from `boxBlur`, recycle temporary bitmaps with `onTrimMemory`, and window `HomeScreen` card rows.
- TV player: remove the on-screen Play/Pause button so focus sits directly on the timeline bar (OK toggles play/pause, Left/Right skips or scrubs, Up/Down opens controls), add a Netflix-style 5-frame pre-decoded thumbnail filmstrip on scrub hold, show "Ends at" time when paused, add a silent-freeze watchdog with staged buffering/reconnecting messages and quiet auto-retry on network drops, prevent playhead rubber-banding on seek, skip idle UI ticks while controls are hidden, and prefer English + hardware-decoded audio over software TrueHD on TV speakers.

## [1.0.0]

The first public release.

### Added

- Engine: one-line install for Linux with systemd, and the commands `install`, `setup`, `upgrade` (checked against `SHA256SUMS`, with automatic rollback), `uninstall`, `doctor` (with `--json` for scripts and agents), `backup`, `backup list` and `restore`.
- Engine: downloads from links, filed into Movies, TV Shows and Music with TMDB posters and details; Dolby Vision profile 7 converted for Android TVs.
- Engine: versioned state. An upgrade that changes stored data backs it up first; an older version refuses newer data instead of damaging it.
- Engine: a nightly backup of progress, settings and devices, with an optional second copy on a USB stick or share.
- TV app and Phone app: find the server on the home network by themselves, play files as they are, share watch progress, skip intros; the phone keeps downloads for offline.
- Site: the TV app's look, a web player, first-run setup with a one-time code, and Manager for downloads and files.

### Security

- Every TV, phone and browser pairs with a code that an admin approves. Two roles: player and admin. Keys are stored only as hashes.
- The server answers only its home network and refuses unknown host names (DNS rebinding).
- The apps send their key only to a server that proves it knows it.
- Releases carry build attestations, and the apps are signed with one key in a protected environment.
