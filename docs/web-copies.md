# Browser copies

Browsers can't reliably play the library's files (MKV, HEVC, E-AC-3, DTS), so the web player plays a lighter copy of each title. The engine serves copies and the player plays them. **Nothing makes them automatically yet**: that's the queue still to build, and until then `scripts/web-copy.py` makes one by hand. A title with no copy shows "Not ready for browsers", and the TV and phone apps never use copies.

## What the player needs

One folder per item, at `~/.local/share/media-core/web/<item id>/`. It's an id from `/api/v1/item`: a film or an episode.

| File | What |
|---|---|
| `master.m3u8` | The HLS entry point. Video is one variant, and each audio track is an alternate rendition in the same order as `audio` below. |
| `p*.m3u8`, `t*.m4s` | One playlist and one single fMP4 file per track, addressed by byte ranges. |
| `s<n>.vtt` / `s<n>.sup` | Subtitles: WebVTT for text, PGS for images. |
| `manifest.json` | Written last, because its presence says the copy is whole. Build in a temp folder and rename it into place. |

```json
{
  "source_size": 2636872794,
  "source_mtime": 1792807252,
  "audio": [{ "lang": "hin", "title": "Hindi | DDP 2.0", "codec": "eac3", "channels": 2 }],
  "subs": [{ "file": "s0.sup", "lang": "eng", "title": "PGS | English | SDH", "forced": false }]
}
```

`source_size` and `source_mtime` (Unix seconds) are the original file's. If the file is later replaced, the copy stops counting.

Video is 1080p at most, 8-bit H.264. Audio is stereo AAC. HDR has to be tone-mapped to SDR. Keep a keyframe at least every 6 s, so seeks stay quick. `scripts/web-copy.py` has the exact ffmpeg command.

## What the engine serves

On :8780 only (`engine/webcopy.go`):

- `GET /api/v1/web`: `{"ready": {"<id>": true}}`
- `GET /api/v1/web/{id}`: the manifest with `"state": "ready"`, or `{"state": "none"}`
- `GET /api/v1/web/{id}/{file}`: the files, with ranges. Fetching `.m4s` counts as playing, so background jobs pause.

## Still to build

The queue: find titles without a current copy, make them one at a time at low priority, pause during playback as thumbnails do, and remove copies whose source has gone. The title page can then show progress in place of "Not ready for browsers", using an extra `state` such as `"preparing"`.

Dolby Vision profile 5 has no HDR10 base layer, so the tone mapping above would give wrong colours. Nothing in the library has it today.
