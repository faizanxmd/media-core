package main

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	Listen = ":8780" // ufw only lets tailscale0 reach it
	UA     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"
	// MB/s used for ETAs until a real download has been measured.
	DefaultMBps = 3.5
)

var (
	// Media is the library root. MEDIA_CORE_LIBRARY overrides it, e.g. to run
	// the engine against a small test library on a dev machine.
	Media     = envOr("MEDIA_CORE_LIBRARY", "/srv/data/media")
	home      = os.Getenv("HOME")
	DataDir   = filepath.Join(home, ".local/share/media-core")
	JobsDir   = filepath.Join(DataDir, "jobs")
	ConfDir   = filepath.Join(home, ".config/media-core")
	Aria2Conf = filepath.Join(ConfDir, "aria2.conf")
	DoviTool  = filepath.Join(home, ".local/bin/dovi_tool")

	VideoExt   = set("mkv", "mp4", "m4v", "avi", "mov", "ts", "m2ts", "webm")
	AudioExt   = set("flac", "mp3", "m4a", "aac", "ogg", "opus", "wav", "alac", "aiff")
	ArchiveExt = set("zip", "rar", "7z")
	SubExt     = set("srt", "ass", "ssa", "sub", "idx", "vtt")
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return strings.TrimRight(v, "/")
	}
	return def
}

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// ext returns the lower-case extension without the dot.
func ext(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

// rel shows a library path the way the UI does: relative to /srv/data/media.
func rel(p string) string { return strings.TrimPrefix(p, Media+"/") }
