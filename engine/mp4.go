package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Legacy browser-compatibility fallback (when a browser sends no ?can= list)
// and shared helpers for Light copies and stream probing.

var browserAudio = set("aac", "mp3", "opus", "flac")

func browserVideo(m *MInfo) bool {
	return m != nil && (strings.HasPrefix(m.VCodec, "H.264") || strings.HasPrefix(m.VCodec, "HEVC")) &&
		m.Res != "4K" && m.HDR == ""
}

// playsInBrowser: the file plays in a browser as it is (fallback when ?can= is absent).
func playsInBrowser(m *MInfo) bool {
	return browserVideo(m) && (m.Container == "mp4" || m.Container == "m4v") && browserAudio[m.ACodec]
}

// heavy: 4K or HDR, the files a Light copy is for.
func heavy(m *MInfo) bool { return m != nil && (m.Res == "4K" || m.HDR != "") }

// ---------- Light copies ----------

// A title's Light copy sits in a hidden .light folder beside its file, with
// the same base name: "Movies/X (2020)/.light/X (2020).mp4". The catalog skips
// dot-folders, so it never shows up as a title of its own.
func lightDir(video string) string { return filepath.Join(filepath.Dir(video), ".light") }

func baseName(p string) string { return strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)) }

// findLight returns the Light copy of a video, or "".
func findLight(video string) string {
	ents, _ := os.ReadDir(lightDir(video))
	return findLightFrom(video, ents)
}

func findLightFrom(video string, ents []os.DirEntry) string {
	base := baseName(video)
	for _, e := range ents {
		if !e.IsDir() && VideoExt[ext(e.Name())] && baseName(e.Name()) == base {
			return filepath.Join(lightDir(video), e.Name())
		}
	}
	return ""
}

// mainVideo finds the film or episode a Light job belongs to: into is the
// main file's library path (any extension, since MP4 conversion may change
// it) or a film's folder.
func mainVideo(into string) string {
	abs := filepath.Join(Media, into)
	if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
		best := ""
		var bestSize int64
		ents, _ := os.ReadDir(abs)
		for _, e := range ents {
			if fi, err := e.Info(); err == nil && !e.IsDir() && VideoExt[ext(e.Name())] && fi.Size() > bestSize {
				best, bestSize = filepath.Join(abs, e.Name()), fi.Size()
			}
		}
		return best
	}
	base := baseName(abs)
	ents, _ := os.ReadDir(filepath.Dir(abs))
	for _, e := range ents {
		if !e.IsDir() && VideoExt[ext(e.Name())] && baseName(e.Name()) == base {
			return filepath.Join(filepath.Dir(abs), e.Name())
		}
	}
	return ""
}

func duration(ctx context.Context, p string) float64 {
	d, _ := strconv.ParseFloat(ffprobe(ctx, p, "-show_entries", "format=duration"), 64)
	return d
}

// sameCut: a Light copy must run as long as its film (within 2 s), or resume
// positions, Skip Intro and scrub pictures would all land in the wrong place.
func sameCut(a, b float64) bool { return a > 0 && b > 0 && math.Abs(a-b) <= 2 }

// ---------- making an MP4 ----------

type probeStream struct {
	Index    int    `json:"index"`
	Type     string `json:"codec_type"`
	Codec    string `json:"codec_name"`
	Profile  string `json:"profile"`
	Channels int    `json:"channels"`
	Tags     struct {
		Language string `json:"language"`
		Title    string `json:"title"`
		Handler  string `json:"handler_name"` // where an MP4 keeps a track's name
	} `json:"tags"`
	Disp struct {
		Default int `json:"default"`
		Forced  int `json:"forced"`
		Pic     int `json:"attached_pic"`
	} `json:"disposition"`
}

func probeStreams(ctx context.Context, p string) ([]probeStream, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries",
		"stream=index,codec_type,codec_name,profile,channels:stream_tags=language,title,handler_name:stream_disposition=default,forced,attached_pic",
		"-of", "json", p).Output()
	if err != nil {
		return nil, fmt.Errorf("can't read %s", filepath.Base(p))
	}
	var r struct{ Streams []probeStream }
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	for i, s := range r.Streams {
		if h := s.Tags.Handler; s.Tags.Title == "" && h != "" && !strings.HasSuffix(h, "Handler") {
			r.Streams[i].Tags.Title = h
		}
	}
	return r.Streams, nil
}

// probeStreamsKept: the same, remembered until the file changes. Opening a
// title asks for its tracks every time, and ffprobe is a disk seek on the server.
var (
	probeKeptMu sync.Mutex
	probeKept   = map[string]keptProbe{}
)

type keptProbe struct {
	size, mtime int64
	streams     []probeStream
}

func probeStreamsKept(ctx context.Context, p string) ([]probeStream, error) {
	size, mtime, ok := fileKey(p)
	probeKeptMu.Lock()
	k, have := probeKept[p]
	probeKeptMu.Unlock()
	if ok && have && k.size == size && k.mtime == mtime {
		return k.streams, nil
	}
	s, err := probeStreams(ctx, p)
	if err == nil && ok {
		probeKeptMu.Lock()
		if len(probeKept) > 500 {
			clear(probeKept)
		}
		probeKept[p] = keptProbe{size, mtime, s}
		probeKeptMu.Unlock()
	}
	return s, err
}

var textSubs = set("subrip", "ass", "ssa", "webvtt", "mov_text", "text")

func isEnglish(lang string) bool {
	l := strings.ToLower(lang)
	return l == "eng" || l == "en" || strings.HasPrefix(l, "en-")
}

func isCommentary(title string) bool { return strings.Contains(strings.ToLower(title), "comment") }
