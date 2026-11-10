# Playing in a browser

The web player at `mint:8780` plays what this browser can decode, and the browser says what that is. When the page loads, it asks
the browser which picture and sound formats it plays (`browserCan` in `web/player.js`: H.264, HEVC, AV1, VP9, HDR, Dolby Vision,
AAC, Dolby Digital, DD+, FLAC and more, and whether it can switch audio tracks) and sends the list with every question as `?can=`.
The engine decides with it (`howToPlay` in `engine/play.go`, with a test table of format cases in `play_test.go`).

Two rules hold throughout: a library file is never rewritten to suit a browser, and the picture is never converted while it
plays (mint's CPU can't).

## How a browser gets a title, cheapest first

1. **Direct**: the file as it is, from `/api/v1/stream/{id}` with byte ranges. An MP4 whose picture and sound this browser plays.
2. **Repackaged while it plays**: when the browser decodes the picture but can't take the file as it is. The picture is copied
   untouched into an HLS stream, each sound track is copied when the browser plays it and made stereo AAC when it doesn't.
   Nothing is written to disk.
3. **Its Light copy**, direct or repackaged, when the browser can't decode the picture itself (`?v=light`).
4. Otherwise the player says it can't play here and that a Light copy would.

What that means on the user's Mac, tested 2026-11-10 with sample clips and real files:

| | Safari | Chrome |
|---|---|---|
| MP4, sound the browser plays | direct | direct |
| MP4 with Dolby sound | direct | repackaged, sound made AAC |
| MKV (any) | repackaged; Safari opens no MKV at all | repackaged; Chrome opens MKV but has no Dolby or DTS sound |
| Dolby Vision 8.1 | plays | plays as HDR10 |
| Dolby Vision 5 | plays | Light copy (it has no HDR10 underneath) |
| AV1 | Light copy on an M1 | plays |

A page that sends no `?can=` gets the old fixed rule (`playsInBrowser` in `engine/mp4.go`) and never a stream.

## The stream (`engine/stream.go`, `stream_http.go`, `keyframes.go`)

- **Where it's cut.** Only at the file's keyframes, read from the file's own index in a few milliseconds: an MKV's Cues, an MP4's
  sample tables. Picture pieces are at least 2 s (4 s for small pictures); sound pieces are 2.048 s, a whole number of AAC frames.
  A file with no index is read through once with ffprobe (minutes), and the player says so meanwhile.
- **Who cuts it.** One ffmpeg per track writes a fragmented MP4 to a pipe; the engine files each fragment under the piece its
  time falls in. So a piece is the same bytes wherever a run started, and the playlists are exact. The flags that keep ffmpeg's
  timestamps the same from run to run are explained at `args` in `stream.go`.
- **Jumps.** A piece that isn't on its way stops the track's ffmpeg and starts one at that piece. On mint, with the 30 GB
  Dolby Vision film: about 0.3 s for the picture and 0.4 s for DTS-HD sound, of which only the plain DTS layer is decoded.
- **Memory, not disk.** Pieces live in memory, up to 160 MB of picture around the player's position. ffmpeg is held (SIGSTOP)
  once it's 45 s or 64 MB ahead, and stopped after two minutes without requests.
- **Checked against the files.** At several positions in an MKV film, an MKV episode with 10 s between keyframes and an MP4,
  the stream's first picture is the file's frame and its sound sits within 1 ms of the file's.

Addresses, on :8780 only: `/api/v1/hls/{id}/{which}/master.m3u8?can=…`, then `v.m3u8`, `a<n>t.m3u8` (track n made AAC) or
`a<n>c.m3u8` (copied), and their `-init.mp4` and `-<piece>.m4s`. `which` is `o` (the file) or `l` (its Light copy) plus the
file's change time, so a browser's kept pieces don't outlive the file. `STREAM_DEBUG=1` logs every run and piece.

To try a file outside the engine: `STREAM_FILE=/path/film.mkv STREAM_FOR=120 go test -run TestServeRealStream -timeout 0`,
then open `http://127.0.0.1:18799/master.m3u8?can=h264,hevc,aac`.

## Still made ahead of time

- **A Light copy** is a second, smaller download: H.264 or HEVC, 1080p at most, no HDR. It lives in a hidden `.light` folder beside
  the file, with the same base name (`Movies/X (2020)/.light/X (2020).mp4`). It's kept only if it runs as long as the film (within
  2 s). Deleting the title deletes it too. It's for pictures a device can't decode and for slow connections.
- **Make it MP4** (Manager) rewrites a file as an MP4 for good. Browsers no longer need it, and it still turns DTS and TrueHD into
  AAC, which the TV loses, so it's on its way out.
- **An old HLS browser copy** in `~/.local/share/media-core/web/<id>/`, made by hand before any of this. The one left is True
  Detective S01E01; the stream replaces it.

## What the engine tells the web player (`engine/webcopy.go`)

- `GET /api/v1/web`: `{"ready": {"<id>": true}}`, every item this browser can play, by any of the ways above.
- `GET /api/v1/web/{id}`: `{"state": "ready"}` with `src` (direct) or `hls` (the stream's master playlist), the sound tracks in
  the file's order and the subtitle files; `{"state": "wait", "wait": "…"}` while a file without an index is read; or
  `{"state": "none"}`.
- `GET /api/v1/web/{id}/{file}`: subtitles (`x<n>.vtt` from the `.srt` files beside the video, `p<n>.sup` for picture subtitles) or
  an old HLS copy's files.

## Not done yet

- Subtitles inside an MKV don't reach browsers yet; only `.srt` files beside the video do.
- Dolby Vision 5 in Safari through the stream is labelled as Dolby Vision (`dvh1`) but hasn't been tried on a real file.
- Safari and Chrome on the Mac haven't played the finished stream; the first test of this idea there used hand-made pieces.
  Firefox on mint has: start, jumps, a sound-track switch and two minutes of playback.
