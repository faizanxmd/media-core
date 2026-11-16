<div align="center">

<img src="engine/web/icon.svg" width="96" alt="">

# Media Core

Your own library, downloader and player, for your TV, phone and browser.

</div>

## What it is

Media Core turns a computer at home into your own streaming service. Paste a link, and it downloads the file, names it and files it into your library, with posters and details from TMDB. The TV app, the phone app and the website then play that library, with watch progress shared between them.

Files play as they are. The server never re-encodes while you watch, so a 4K Dolby Vision file reaches the TV untouched.

## Install

On a Linux computer (or a Mac) at home:

```sh
curl -fsSL https://github.com/faizanxmd/media-core/releases/latest/download/install.sh | sh
```

It ends with a link and a setup code: open the link in a browser at home and enter the code. Then install the apps from `http://<server>:8790/` and pair each with the code it shows. [docs/setup.md](docs/setup.md) has every step, and [AGENTS.md](AGENTS.md) is the same for an AI agent doing it for you.

`media-core-engine doctor` checks everything and says how to fix what isn't right. `media-core-engine upgrade` moves to the newest release and puts the old one back if anything goes wrong.

## Downloads

Each [release](https://github.com/faizanxmd/media-core/releases/latest) has the server for Linux and macOS (amd64 and arm64) and the TV and phone apps, with checksums and build attestations. The server hands the apps out itself, and they update from it.

## The parts

- [`engine/`](engine): the server, in Go with only the standard library. It downloads, files and streams the library, and includes the website ([`engine/web`](engine/web)).
- [`android/app`](android/app): the Android TV app, Kotlin with Jetpack Compose and Media3.
- [`android/phone`](android/phone): the phone app, with downloads for offline.
- [`android/core`](android/core): what both apps use to talk to the server.

[docs/architecture.md](docs/architecture.md) explains how they fit together, [docs/api.md](docs/api.md) lists the API, and [SECURITY.md](SECURITY.md) explains who can reach what.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) covers building, testing and sending changes, and [docs/development.md](docs/development.md) the details.

## Credits

- [TMDB](https://www.themoviedb.org) for titles, posters and details. This product uses the TMDB API but is not endorsed or certified by TMDB.
- [aria2](https://aria2.github.io), [FFmpeg](https://ffmpeg.org), [MKVToolNix](https://mkvtoolnix.download), [7-Zip](https://www.7-zip.org) and [dovi_tool](https://github.com/quietvoid/dovi_tool) do the downloading and file work.
- [AndroidX Media3](https://developer.android.com/media/media3) plays everything in the apps, and [hls.js](https://github.com/video-dev/hls.js) in the browser.
- Fonts, icons and their licences are listed in [android/brand/README.md](android/brand/README.md).

## License

Media Core is licensed under the GNU General Public License v3.0. See [LICENSE](LICENSE). Code and assets from other projects keep their own licences.
