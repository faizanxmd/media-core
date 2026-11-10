package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Where a file plays. The user's browsers all play HEVC, and 4K HDR isn't
// wanted in a browser (1080p is enough), so a browser plays an MP4 of H.264 or
// HEVC up to 1080p, without HDR, whose first audio track it can decode.

var browserAudio = set("aac", "mp3", "opus", "flac")

// mp4Audio can be copied into an MP4 as it is; anything else becomes AAC.
var mp4Audio = set("aac", "mp3", "opus", "flac", "ac3", "eac3", "alac")

func browserVideo(m *MInfo) bool {
	return m != nil && (strings.HasPrefix(m.VCodec, "H.264") || strings.HasPrefix(m.VCodec, "HEVC")) &&
		m.Res != "4K" && m.HDR == ""
}

// playsInBrowser: the file plays in a browser as it is.
func playsInBrowser(m *MInfo) bool {
	return browserVideo(m) && (m.Container == "mp4" || m.Container == "m4v") && browserAudio[m.ACodec]
}

// canMP4: turning the file into an MP4 (video untouched) makes it play in browsers.
func canMP4(m *MInfo) bool { return browserVideo(m) && !playsInBrowser(m) }

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
	base := baseName(video)
	ents, _ := os.ReadDir(lightDir(video))
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

var textSubs = set("subrip", "ass", "ssa", "webvtt", "mov_text", "text")

// mp4Args builds the ffmpeg command that rewrites a file as an MP4: the video
// copied untouched, audio copied where MP4 can hold it (DTS, TrueHD and PCM
// become AAC), and a browser-safe AAC version of the main track put first when
// the main track isn't one a browser plays (a browser plays only the first).
// Text subtitles become .srt files beside the video and picture subtitles .sup
// files, because MP4 can't carry them in a way every player reads.
// subs maps each subtitle output to its final name.
func mp4Args(src, out string, streams []probeStream) (args []string, subs map[string]string, err error) {
	video := -1
	var audio []probeStream
	for _, s := range streams {
		switch {
		case s.Type == "video" && video < 0 && s.Disp.Pic == 0 && s.Codec != "mjpeg" && s.Codec != "png":
			video = s.Index
		case s.Type == "audio":
			audio = append(audio, s)
		}
	}
	if video < 0 {
		return nil, nil, errors.New("no video stream")
	}
	// the main track first: English, then the one marked default, else the
	// first. Foreign releases (a French WEB-DL with the English original as a
	// second track) mark their own dub default, and the browser-safe AAC copy
	// and every player's first pick follow the main track.
	main := func(s probeStream) int {
		r := s.Disp.Default
		if isEnglish(s.Tags.Language) && !isCommentary(s.Tags.Title) {
			r += 2
		}
		return r
	}
	slices.SortStableFunc(audio, func(a, b probeStream) int { return main(b) - main(a) })

	args = []string{"-nostdin", "-loglevel", "error", "-nostats", "-progress", "pipe:2", "-i", src,
		"-map", "0:" + itoa(video), "-c:v", "copy"}
	vcodec := ""
	for _, s := range streams {
		if s.Index == video {
			vcodec = s.Codec
		}
	}
	if vcodec == "hevc" {
		args = append(args, "-tag:v", "hvc1") // what Apple and Chrome look for
	}
	n := 0
	named := func(s probeStream) { // MP4 keeps a track's name as its handler name
		if s.Tags.Title != "" {
			args = append(args, "-metadata:s:a:"+itoa(n), "handler_name="+s.Tags.Title, "-metadata:s:a:"+itoa(n), "title="+s.Tags.Title)
		}
	}
	addAAC := func(s probeStream, ch int) {
		named(s)
		br := "192k"
		if ch > 2 {
			br = "384k"
		}
		args = append(args, "-map", "0:"+itoa(s.Index), "-c:a:"+itoa(n), "aac", "-b:a:"+itoa(n), br, "-ac:a:"+itoa(n), itoa(ch))
		n++
	}
	if len(audio) > 0 && !browserAudio[audio[0].Codec] {
		addAAC(audio[0], 2)
	}
	for _, s := range audio {
		if mp4Audio[s.Codec] {
			named(s)
			args = append(args, "-map", "0:"+itoa(s.Index), "-c:a:"+itoa(n), "copy")
			n++
		} else {
			addAAC(s, min(max(s.Channels, 2), 6))
		}
	}
	for i := range n {
		d := "0"
		if i == 0 {
			d = "default"
		}
		args = append(args, "-disposition:a:"+itoa(i), d)
	}
	args = append(args, "-sn", "-dn", "-map_chapters", "0", "-strict", "experimental",
		"-max_muxing_queue_size", "4096", "-movflags", "+faststart", "-f", "mp4", out)

	subs = map[string]string{}
	base := baseName(src)
	used := map[string]bool{}
	work := filepath.Dir(out)
	k := 0
	for _, s := range streams {
		if s.Type != "subtitle" {
			continue
		}
		kind := ""
		switch {
		case textSubs[s.Codec]:
			kind = "srt"
		case s.Codec == "hdmv_pgs_subtitle":
			kind = "sup"
		default:
			continue
		}
		lang := strings.ToLower(s.Tags.Language)
		if lang == "" || lang == "und" {
			lang = "und"
		}
		name := base + "." + lang
		if t := strings.ToLower(s.Tags.Title); strings.Contains(t, "sdh") {
			name += ".sdh"
		}
		if s.Disp.Forced == 1 || strings.Contains(strings.ToLower(s.Tags.Title), "forced") {
			name += ".forced"
		}
		final := name + "." + kind
		for i := 2; used[final]; i++ {
			final = fmt.Sprintf("%s.%d.%s", name, i, kind)
		}
		used[final] = true
		tmp := filepath.Join(work, fmt.Sprintf("sub%d.%s", k, kind))
		k++
		if kind == "srt" {
			args = append(args, "-map", "0:"+itoa(s.Index), "-c:s", "srt", "-f", "srt", tmp)
		} else {
			args = append(args, "-map", "0:"+itoa(s.Index), "-c:s", "copy", "-f", "sup", tmp)
		}
		subs[tmp] = final
	}
	return args, subs, nil
}

// toMP4 rewrites a library file as an MP4 beside it, checks the result and
// only then replaces the original. Returns the new path.
func (e *Engine) toMP4(ctx context.Context, c jobCtx, path string) (string, error) {
	if ext(path) == "mp4" {
		return path, nil
	}
	final := strings.TrimSuffix(path, filepath.Ext(path)) + ".mp4"
	if exists(final) {
		return "", fmt.Errorf("%s already exists", filepath.Base(final))
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if free, _ := diskAt(filepath.Dir(path)); free > 0 && free < fi.Size()+2e9 {
		return "", fmt.Errorf("not enough free space to make the MP4: needs %s, %s free", human(fi.Size()+2e9), human(free))
	}
	streams, err := probeStreams(ctx, path)
	if err != nil {
		return "", err
	}
	work := filepath.Join(filepath.Dir(path), ".mp4-"+baseName(path))
	os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o2775); err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	out := filepath.Join(work, "new.mp4")
	args, subs, err := mp4Args(path, out, streams)
	if err != nil {
		return "", err
	}
	c.phase(Processing, "Making MP4")
	c.log("Making MP4: picture copied untouched")
	srcDur := duration(ctx, path)
	ff := lowPrio(ctx, "ffmpeg", args...)
	stderr, _ := ff.StderrPipe()
	if err := ff.Start(); err != nil {
		return "", err
	}
	prog := c.progressFn()
	var tail strings.Builder
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		if m := reOutTime.FindStringSubmatch(sc.Text()); m != nil && srcDur > 0 {
			us, _ := strconv.ParseFloat(m[1], 64)
			prog(int(us / 1e6 / srcDur * 100))
		} else if !strings.Contains(sc.Text(), "=") {
			tail.WriteString(sc.Text() + "\n")
		}
	}
	if err := ff.Wait(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("making the MP4 failed: %s", last(strings.TrimSpace(tail.String()), 300))
	}
	if d := duration(ctx, out); !sameCut(d, srcDur) {
		return "", fmt.Errorf("check failed: the MP4 runs %.0f s, the original %.0f s; kept the original", d, srcDur)
	}
	if m := probeMediaCtx(ctx, out); m == nil || m.VCodec == "" || m.ACodec == "" && hasAudio(streams) {
		return "", errors.New("check failed: the MP4 is missing its picture or sound; kept the original")
	}
	dir := filepath.Dir(path)
	for tmp, name := range subs {
		if dst := filepath.Join(dir, name); !exists(dst) {
			move(tmp, dst)
		}
	}
	if err := os.Rename(out, final); err != nil {
		return "", err
	}
	os.Remove(path)
	if e.onMoved != nil && !strings.Contains(rel(path), "/.") {
		e.onMoved(path, final)
	}
	c.log("Made " + filepath.Base(final))
	return final, nil
}

func isEnglish(lang string) bool {
	l := strings.ToLower(lang)
	return l == "eng" || l == "en" || strings.HasPrefix(l, "en-")
}

func isCommentary(title string) bool { return strings.Contains(strings.ToLower(title), "comment") }

func hasAudio(streams []probeStream) bool {
	for _, s := range streams {
		if s.Type == "audio" {
			return true
		}
	}
	return false
}
