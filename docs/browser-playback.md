# Playing in a browser

The web player at `mint:8780` plays what this browser can decode, and the browser says what that is. When the page loads, it asks
the browser which picture and sound formats it plays (`browserCan` in `web/player.js`: H.264, HEVC, AV1, VP9, HDR, Dolby Vision,
AAC, Dolby Digital, DD+, FLAC and more, and whether it can switch audio tracks) and sends the list with every question as `?can=`.
The engine decides with it (`engine/browser.go`, `playsIn`): an MP4 whose picture this browser decodes, including its HDR or Dolby
Vision, and whose sound it can play: the first track, or any track when it can switch tracks. There's no resolution limit.

Safari on the user's M1 MacBook Air is the browser this is designed for: it plays 4K HEVC, HDR, Dolby Vision, DD+ and switches tracks,
so most MP4s play as they are. Chrome can't play Dolby audio or switch tracks, so it needs an AAC first track. A page that sends
nothing gets the old fixed rule (`playsInBrowser` in `engine/mp4.go`: 1080p at most, no HDR, AAC, MP3, Opus or FLAC first).
MKV never plays in a browser: Make MP4 rewraps it. Make MP4 is still offered only for 1080p SDR files until it keeps the
original sound tracks (it turns DTS and TrueHD into AAC, which the TV would lose).

## What a browser plays, in order

1. **The library file itself**, when it follows the rule. It streams from `/api/v1/stream/{id}` with byte ranges.
2. **Its Light copy**, when the file doesn't follow the rule but the Light copy does (`/api/v1/stream/{id}?v=light`).
3. **An old HLS browser copy** in `~/.local/share/media-core/web/<id>/`, the format made by hand before Manager existed.
   Nothing makes these any more; the one left is True Detective S01E01, whose MKV keeps its PGS subtitles.

Otherwise the title says "Not ready for browsers", and Manager offers what would fix it.

## Making a title playable

- **Make it MP4** (Manager, when a link is checked or from the Library): the video is copied untouched. Audio MP4 can hold is copied
  (AAC, MP3, Opus, FLAC, AC-3, E-AC-3, ALAC), and DTS, TrueHD and PCM become AAC. If the main track isn't one a browser plays, an AAC
  stereo copy of it goes first, because a browser plays the first track. Text subtitles become `.srt` files beside the video, and picture
  (PGS) subtitles `.sup` files, which only the web player reads: the TV and phone lose those. The MP4 replaces the original only after
  its length checks out, and the title's watch progress moves to its new id.
- **A Light copy** is a second, smaller download: H.264 or HEVC, 1080p at most, no HDR. It lives in a hidden `.light` folder beside the
  file, with the same base name (`Movies/X (2020)/.light/X (2020).mp4`). It's kept only if it runs as long as the film (within 2 s);
  it's made an MP4 if that's what lets it play in browsers. Deleting the title deletes it too.

## What the engine serves (`engine/webcopy.go`)

On :8780 only:

- `GET /api/v1/web`: `{"ready": {"<id>": true}}`, every item a browser can play.
- `GET /api/v1/web/{id}`: `{"state": "ready", "src": "/api/v1/stream/…"}` with its audio tracks and subtitles, or an HLS copy's manifest
  (no `src`), or `{"state": "none"}`.
- `GET /api/v1/web/{id}/{file}`: subtitles (`x<n>.vtt` from the `.srt` files beside the video, `p<n>.sup` for picture subtitles) or an
  HLS copy's files.
