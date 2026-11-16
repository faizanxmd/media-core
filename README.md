<div align="center">

<img src="engine/web/icon.svg" width="96" alt="">

# Media Core

Your own library, downloader and player, for your TV, phone and browser.

</div>

## What it is

Media Core runs on a Linux box at home. You paste a link, and it downloads the file, names it and files it into your library, with posters and details from TMDB. The TV app, the phone app and the website then play that library, with watch progress shared between them.

Files play as they are. The engine never re-encodes while you watch, so a 4K Dolby Vision file reaches the TV untouched.

## Get it

The engine is installed from this repo: follow [docs/setup.md](docs/setup.md). Once it runs, it serves the apps itself at `http://<server>:8790/`, and each app updates itself from there.

| Part | Where |
| --- | --- |
| media-core-engine (server, website and Manager) | [`engine/`](engine), set up with [docs/setup.md](docs/setup.md) |
| TV app (Android TV) | `http://<server>:8790/api/v1/app.apk` |
| Phone app (Android) | `http://<server>:8790/api/v1/phone-app.apk` |

## The parts

- [`engine/`](engine): the server, in Go with only the standard library. It downloads through aria2, unpacks, renames, fixes Dolby Vision, keeps the catalog, records watch progress, finds intros, streams and serves WebDAV. The website and Manager ([`engine/web`](engine/web)) are built into it.
- [`android/app`](android/app): the TV app, Kotlin with Jetpack Compose and Media3.
- [`android/phone`](android/phone): the phone app, with downloads for offline.
- [`android/core`](android/core): what both apps use to talk to the engine.
- [`deploy/`](deploy): the install script, systemd user units and the backup script.

[docs/architecture.md](docs/architecture.md) explains how they fit together, and how the apps find the server.

## Development

See [docs/development.md](docs/development.md) for building and testing the engine and the apps.

## Credits

- [TMDB](https://www.themoviedb.org) for titles, posters and details. This product uses the TMDB API but is not endorsed or certified by TMDB.
- [aria2](https://aria2.github.io), [FFmpeg](https://ffmpeg.org), [MKVToolNix](https://mkvtoolnix.download), [7-Zip](https://www.7-zip.org) and [dovi_tool](https://github.com/quietvoid/dovi_tool) do the downloading and file work.
- [AndroidX Media3](https://developer.android.com/media/media3) plays everything in the apps, and [hls.js](https://github.com/video-dev/hls.js) in the browser.
- [Tailscale](https://tailscale.com) for use away from home.
- Fonts and icons: Source Sans 3, Inter, Montserrat and Fluent UI System Icons. Their licences are listed in [android/brand/README.md](android/brand/README.md).

## License

Media Core is licensed under the GNU General Public License v3.0. See [LICENSE](LICENSE). Code and assets from other projects keep their own licences.
