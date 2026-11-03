package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWherePlays(t *testing.T) {
	cases := []struct {
		m                       MInfo
		browser, mp4, heavyCopy bool
	}{
		{MInfo{VCodec: "HEVC Main 10", Res: "1080p", Container: "mp4", ACodec: "aac"}, true, false, false},
		{MInfo{VCodec: "H.264", Res: "1080p", Container: "mkv", ACodec: "flac"}, false, true, false},
		{MInfo{VCodec: "H.264", Res: "1080p", Container: "mp4", ACodec: "ac3"}, false, true, false},
		{MInfo{VCodec: "HEVC Main 10", Res: "4K", HDR: "Dolby Vision 8.1", Container: "mkv", ACodec: "dts"}, false, false, true},
		{MInfo{VCodec: "HEVC", Res: "1080p", HDR: "HDR10", Container: "mp4", ACodec: "aac"}, false, false, true},
		{MInfo{VCodec: "AV1", Res: "1080p", Container: "mkv", ACodec: "opus"}, false, false, false},
	}
	for i, c := range cases {
		if playsInBrowser(&c.m) != c.browser || canMP4(&c.m) != c.mp4 || heavy(&c.m) != c.heavyCopy {
			t.Errorf("case %d: browser %v mp4 %v heavy %v", i, playsInBrowser(&c.m), canMP4(&c.m), heavy(&c.m))
		}
	}
}

func TestLightAndConvertPaths(t *testing.T) {
	old := Media
	Media = "/lib"
	defer func() { Media = old }()
	_, fn, final, err := planPaths(Plan{Kind: "movie", Light: true, Into: "Movies/X (2020)/X (2020).mkv", Ext: "mp4"})
	if err != nil || fn != "X (2020).mp4" || final != "/lib/Movies/X (2020)/.light/X (2020).mp4" {
		t.Fatalf("film file: %q %q %v", fn, final, err)
	}
	_, _, final, _ = planPaths(Plan{Kind: "movie", Light: true, Into: "Movies/X (2020)", Ext: "mp4"})
	if final != "/lib/Movies/X (2020)/.light/X (2020).mp4" {
		t.Fatalf("film folder: %q", final)
	}
	_, _, final, _ = planPaths(Plan{Kind: "tv", Light: true, Into: "TV Shows/S/Season 01/S - S01E02.mkv", Ext: "mkv"})
	if final != "/lib/TV Shows/S/Season 01/.light/S - S01E02.mkv" {
		t.Fatalf("episode: %q", final)
	}
	if _, _, _, err := planPaths(Plan{Kind: "movie", Light: true, Into: "Movies/X", Archive: true}); err == nil {
		t.Fatal("an archive can't be a Light copy")
	}
	_, _, final, _ = planPaths(Plan{Kind: "convert", Path: "Movies/X (2020)/X (2020).mkv"})
	if final != "/lib/Movies/X (2020)/X (2020).mkv" {
		t.Fatalf("convert: %q", final)
	}
	_, _, final, _ = planPaths(Plan{Kind: "convert", Path: "../../etc/passwd"})
	if !strings.HasPrefix(final, "/lib/") {
		t.Fatalf("convert escaped the library: %q", final)
	}
}

func TestSrtToVTT(t *testing.T) {
	got := string(srtToVTT([]byte("\ufeff1\r\n00:00:01,500 --> 00:00:02,000\r\nHi\r\n")))
	if got != "WEBVTT\n\n1\n00:00:01.500 --> 00:00:02.000\nHi\n" {
		t.Fatalf("%q", got)
	}
}

func TestMP4ArgsPutsABrowserTrackFirst(t *testing.T) {
	st := []probeStream{
		{Index: 0, Type: "video", Codec: "hevc"},
		{Index: 1, Type: "audio", Codec: "ac3", Channels: 6},
		{Index: 2, Type: "audio", Codec: "dts", Channels: 6},
		{Index: 3, Type: "subtitle", Codec: "subrip"},
		{Index: 4, Type: "subtitle", Codec: "hdmv_pgs_subtitle"},
	}
	st[2].Disp.Default = 1
	st[3].Tags.Language = "eng"
	st[4].Tags.Language = "eng"
	args, subs, err := mp4Args("/x/F.mkv", "/x/w/new.mp4", st)
	if err != nil {
		t.Fatal(err)
	}
	a := strings.Join(args, " ")
	// default DTS first as browser AAC stereo, then DTS as AAC 5.1, then AC3 copied
	for _, want := range []string{"-map 0:2 -c:a:0 aac -b:a:0 192k -ac:a:0 2", "-map 0:2 -c:a:1 aac -b:a:1 384k -ac:a:1 6",
		"-map 0:1 -c:a:2 copy", "-tag:v hvc1", "-disposition:a:0 default", "-disposition:a:1 0"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q in %s", want, a)
		}
	}
	names := map[string]bool{}
	for _, n := range subs {
		names[n] = true
	}
	if !names["F.eng.srt"] || !names["F.eng.sup"] {
		t.Errorf("subtitle files: %v", subs)
	}
}

// makeClip writes a short test video with ffmpeg.
func makeClip(t *testing.T, path string, secs string, extra ...string) {
	t.Helper()
	args := append([]string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24", "-f", "lavfi", "-i", "sine=frequency=440",
		"-t", secs, "-map", "0:v", "-map", "1:a", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p"}, extra...)
	if out, err := exec.Command("ffmpeg", append(args, path)...).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg can't make a clip: %s", out)
	}
}

func testEngine(t *testing.T) *Engine {
	st := &Store{jobs: map[string]*Job{}, dirty: map[string]bool{}, subs: map[chan struct{}]bool{}}
	e := &Engine{store: st, settings: &settingsStore{}}
	return e
}

func TestToMP4AndLight(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()
	dir := filepath.Join(Media, "Movies", "Clip (2020)")
	os.MkdirAll(dir, 0o755)
	src := filepath.Join(dir, "Clip (2020).mkv")
	srt := filepath.Join(t.TempDir(), "s.srt")
	os.WriteFile(srt, []byte("1\n00:00:00,500 --> 00:00:01,500\nHello\n"), 0o644)
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24", "-f", "lavfi", "-i", "sine=frequency=440",
		"-i", srt, "-t", "4", "-map", "0:v", "-map", "1:a", "-map", "2:s", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "flac", "-c:s", "srt", "-metadata:s:s:0", "language=eng", src}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg can't make a clip: %s", out)
	}
	e := testEngine(t)
	moved := ""
	e.onMoved = func(a, b string) { moved = b }
	j := &Job{ID: "t1", State: Processing, Plan: Plan{Kind: "convert", Path: rel(src)}, Files: []string{src}, Log: []string{}}
	e.store.jobs[j.ID] = j
	ctx := context.Background()
	out, err := e.toMP4(ctx, jobCtx{e, j.ID}, src)
	if err != nil {
		t.Fatal(err)
	}
	if exists(src) || !exists(out) || moved != out {
		t.Fatalf("original %v, mp4 %v, moved %q", exists(src), exists(out), moved)
	}
	m := probeMedia(out)
	if !playsInBrowser(m) || m.ACodec != "flac" {
		t.Fatalf("mp4 doesn't play in browsers: %+v", m)
	}
	if !exists(filepath.Join(dir, "Clip (2020).eng.srt")) {
		t.Fatal("subtitles weren't saved beside the MP4")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 2 {
		t.Fatalf("leftovers in the folder: %v", ents)
	}

	// a Light copy of the same length is kept; one of another length is removed
	lightPath := filepath.Join(dir, ".light", "Clip (2020).mkv")
	os.MkdirAll(filepath.Dir(lightPath), 0o755)
	makeClip(t, lightPath, "4.5")
	lj := Job{ID: "t2", Plan: Plan{Kind: "movie", Light: true, Into: rel(dir), MP4: true}}
	e.store.jobs[lj.ID] = &lj
	if err := e.acceptLight(ctx, jobCtx{e, lj.ID}, lj, []string{lightPath}); err != nil {
		t.Fatal(err)
	}
	if got := findLight(out); got != filepath.Join(dir, ".light", "Clip (2020).mp4") {
		t.Fatalf("light copy: %q", got)
	}
	makeClip(t, lightPath, "9")
	if err := e.acceptLight(ctx, jobCtx{e, lj.ID}, lj, []string{lightPath}); err == nil || exists(lightPath) {
		t.Fatalf("a different cut was kept: %v", err)
	}
}
