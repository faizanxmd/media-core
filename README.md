<div align="center">

<img src="engine/web/icon.svg" width="96" alt="">

# Media Core

Your own library, downloader and player, for your TV, phone and browser.

</div>

## Install

On a Linux computer with systemd, at home:

```sh
curl -fsSL https://github.com/faizanxmd/media-core/releases/latest/download/install.sh | sh
```

It asks for nothing as root, and prints the two or three sudo lines it would like you to run. It ends with a link and a setup code: open the link in a browser at home and enter the code. Then install the apps from `http://<server>:8790/` and pair each with the code it shows. [docs/setup.md](docs/setup.md) has every step, and [docs/operating.md](docs/operating.md) is the same for an AI agent doing it for you.

## What it does

- Paste a link and it lands in your library, named, filed and with artwork from TMDB.
- Files play as they are, 4K Dolby Vision included: the server never re-encodes while you watch.
- One watch history across the TV, the phone and the browser.
- Every device pairs once with a code that an admin approves.
- Runs on any Linux box with systemd. No Docker, no database.

## What it isn't

It doesn't transcode the picture, has no accounts or sharing outside the home, and in version 1 doesn't work away from home.

## Downloads

Each [release](https://github.com/faizanxmd/media-core/releases/latest) has the server for Linux and macOS (amd64 and arm64; on a Mac you start it yourself) and the TV and phone apps, with checksums and build attestations. The server hands the apps out itself, and they update from it.

## The parts

- [`engine/`](engine): the server, in Go with only the standard library. It downloads, files and streams the library, and includes the website ([`engine/web`](engine/web)).
- [`android/app`](android/app): the Android TV app, Kotlin with Jetpack Compose and Media3.
- [`android/phone`](android/phone): the phone app, with downloads for offline.
- [`android/core`](android/core): what both apps use to talk to the server.

## Docs

[Setup](docs/setup.md) · [How it works](docs/architecture.md) · [API](docs/api.md) · [Away from home](docs/remote.md) · [Development](docs/development.md) · [Security](SECURITY.md) · [Contributing](CONTRIBUTING.md) · [Operating, for AI agents](docs/operating.md) · [Coding, for AI agents](AGENTS.md)

## Credits

- Designed, directed and tested by the maintainer, with the code written collaboratively using AI coding agents (Claude and Gemini).
- [TMDB](https://www.themoviedb.org) for titles, posters and details. This product uses the TMDB API but is not endorsed or certified by TMDB.
- [aria2](https://aria2.github.io), [FFmpeg](https://ffmpeg.org), [MKVToolNix](https://mkvtoolnix.download), [7-Zip](https://www.7-zip.org) and [dovi_tool](https://github.com/quietvoid/dovi_tool) do the downloading and file work.
- [AndroidX Media3](https://developer.android.com/media/media3) plays everything in the apps, and [hls.js](https://github.com/video-dev/hls.js) in the browser. [ZXing](https://github.com/zxing/zxing) draws the TV's pairing QR code.
- Fonts, icons and their licences are listed in [android/brand/README.md](android/brand/README.md).

## License

Media Core is licensed under the GNU General Public License v3.0. See [LICENSE](LICENSE). Code and assets from other projects keep their own licences.
