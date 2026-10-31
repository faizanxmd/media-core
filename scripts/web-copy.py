#!/usr/bin/env python3
"""Make one browser copy by hand, in the format docs/web-copies.md describes.

The engine's queue will replace this. Run it as its own unit, at low priority:
  systemd-run --user --unit=webcopy-<name> --collect python3 -I scripts/web-copy.py <video> <item id> [seconds]
Seconds cuts a test clip. The folder only appears once the copy is whole.
"""
import json, os, re, shutil, subprocess, sys

video, item_id = sys.argv[1:3]
clip = sys.argv[3:4]
data = os.environ.get("MEDIA_CORE_DATA") or os.path.expanduser("~/.local/share/media-core")
dest = os.path.join(data, "web", item_id)
tmp = dest + ".tmp"
shutil.rmtree(tmp, ignore_errors=True)
os.makedirs(tmp)

probe = json.loads(subprocess.check_output(["ffprobe", "-v", "error", "-of", "json", "-show_streams", video]))
streams = probe["streams"]
v = next(s for s in streams if s["codec_type"] == "video")
tag = lambda s, k: s.get("tags", {}).get(k, "")
audio = [s for s in streams if s["codec_type"] == "audio"][:4]
hdr = v.get("color_transfer") in ("smpte2084", "arib-std-b67")

scale = "scale=w='trunc(min(1920,iw)/2)*2':h=-2:flags=bicubic"
if hdr:  # browsers would show PQ or HLG washed out
    vf = scale + ",zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p"
else:
    vf = scale + ",format=yuv420p"

cmd = ["ionice", "-c3", "nice", "-n", "19", "ffmpeg", "-nostdin", "-v", "error", "-stats"]
cmd += ["-i", video] + (["-t", clip[0]] if clip else [])
cmd += ["-map", "0:v:0"]
for s in audio:
    cmd += ["-map", f"0:{s['index']}"]
cmd += ["-vf", vf, "-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-maxrate", "10M", "-bufsize", "20M",
        "-force_key_frames", "expr:gte(t,n_forced*6)", "-c:a", "aac", "-b:a", "192k", "-ac", "2",
        "-f", "hls", "-hls_time", "6", "-hls_playlist_type", "vod", "-hls_segment_type", "fmp4",
        "-hls_flags", "single_file+independent_segments", "-master_pl_name", "master.m3u8"]
groups = ["v:0,agroup:aud"] + [f"a:{i},agroup:aud" + (",default:yes" if i == 0 else "") for i in range(len(audio))]
cmd += ["-var_stream_map", " ".join(groups), "-hls_segment_filename", os.path.join(tmp, "t%v.m4s"), os.path.join(tmp, "p%v.m3u8")]

# subtitles: text as WebVTT; images only for a language with no text track
subs, text_langs = [], set()
all_subs = [s for s in streams if s["codec_type"] == "subtitle"]
TEXT = {"subrip", "ass", "ssa", "webvtt", "mov_text", "text"}
for s in all_subs:
    if s["codec_name"] in TEXT:
        text_langs.add(tag(s, "language"))
for s in all_subs:
    lang = tag(s, "language")
    forced = bool(s.get("disposition", {}).get("forced"))
    if s["codec_name"] in TEXT:
        name, codec = f"s{len(subs)}.vtt", ["-c:s", "webvtt", "-f", "webvtt"]
    elif s["codec_name"] == "hdmv_pgs_subtitle" and lang not in text_langs:
        name, codec = f"s{len(subs)}.sup", ["-c:s", "copy", "-f", "sup"]
    else:
        continue
    cmd += ["-map", f"0:{s['index']}"] + codec + [os.path.join(tmp, name)]
    subs.append({"file": name, "lang": lang, "title": tag(s, "title"), "forced": forced})

subprocess.check_call(cmd)

st = os.stat(video)
manifest = {
    "source_size": st.st_size, "source_mtime": int(st.st_mtime),
    "audio": [{"lang": tag(s, "language"), "title": tag(s, "title"), "codec": s["codec_name"], "channels": s.get("channels", 0)} for s in audio],
    "subs": subs,
}
# the manifest goes in last: it's what says the copy is whole
with open(os.path.join(tmp, "manifest.json"), "w") as f:
    json.dump(manifest, f)
shutil.rmtree(dest, ignore_errors=True)
os.rename(tmp, dest)
print("done", dest)
