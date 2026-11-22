package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWherePlays(t *testing.T) {
	cases := []struct {
		m                  MInfo
		browser, heavyCopy bool
	}{
		{MInfo{VCodec: "HEVC Main 10", Res: "1080p", Container: "mp4", ACodec: "aac"}, true, false},
		{MInfo{VCodec: "H.264", Res: "1080p", Container: "mkv", ACodec: "flac"}, false, false},
		{MInfo{VCodec: "H.264", Res: "1080p", Container: "mp4", ACodec: "ac3"}, false, false},
		{MInfo{VCodec: "HEVC Main 10", Res: "4K", HDR: "Dolby Vision 8.1", Container: "mkv", ACodec: "dts"}, false, true},
		{MInfo{VCodec: "HEVC", Res: "1080p", HDR: "HDR10", Container: "mp4", ACodec: "aac"}, false, true},
		{MInfo{VCodec: "AV1", Res: "1080p", Container: "mkv", ACodec: "opus"}, false, false},
	}
	for i, c := range cases {
		if playsInBrowser(&c.m) != c.browser || heavy(&c.m) != c.heavyCopy {
			t.Errorf("case %d: browser %v heavy %v", i, playsInBrowser(&c.m), heavy(&c.m))
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
}

func TestSrtToVTT(t *testing.T) {
	got := string(srtToVTT([]byte("\ufeff1\r\n00:00:01,500 --> 00:00:02,000\r\nHi\r\n")))
	if got != "WEBVTT\n\n1\n00:00:01.500 --> 00:00:02.000\nHi\n" {
		t.Fatalf("%q", got)
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

func TestAcceptLight(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()
	dir := filepath.Join(Media, "Movies", "Clip (2020)")
	os.MkdirAll(dir, 0o755)
	src := filepath.Join(dir, "Clip (2020).mkv")
	makeClip(t, src, "4")

	e := testEngine(t)
	ctx := context.Background()

	// a Light copy of the same length is kept; one of another length is removed
	lightPath := filepath.Join(dir, ".light", "Clip (2020).mkv")
	os.MkdirAll(filepath.Dir(lightPath), 0o755)
	makeClip(t, lightPath, "4.5")
	lj := Job{ID: "t2", Plan: Plan{Kind: "movie", Light: true, Into: rel(dir)}}
	e.store.jobs[lj.ID] = &lj
	if err := e.acceptLight(ctx, jobCtx{e, lj.ID}, lj, []string{lightPath}); err != nil {
		t.Fatal(err)
	}
	if got := findLight(src); got != lightPath {
		t.Fatalf("light copy: %q", got)
	}
	makeClip(t, lightPath, "9")
	if err := e.acceptLight(ctx, jobCtx{e, lj.ID}, lj, []string{lightPath}); err == nil || exists(lightPath) {
		t.Fatalf("a different cut was kept: %v", err)
	}
}

func TestSystemCachesSectionSizes(t *testing.T) {
	oldM, oldD := Media, DataDir
	Media, DataDir = t.TempDir(), t.TempDir()
	defer func() { Media, DataDir = oldM, oldD }()
	os.MkdirAll(filepath.Join(Media, "Movies", "Film"), 0o755)
	os.MkdirAll(filepath.Join(Media, "TV Shows"), 0o755)
	os.MkdirAll(filepath.Join(Media, "Music"), 0o755)
	os.WriteFile(filepath.Join(Media, "Movies", "Film", "f.mkv"), []byte("12345"), 0o644)

	srv := &Server{e: testEngine(t), p: &Player{c: &Catalog{items: map[string]*Item{}}, w: newTestWatch(), t: &TMDB{}}}
	s1 := srv.system()
	lib1 := s1["library"].(map[string]any)
	if lib1["films"].(int64) != 5 {
		t.Fatalf("films = %v, want 5", lib1["films"])
	}

	// Add another file; within 5 minutes /api/system must return the cached size without re-walking.
	os.WriteFile(filepath.Join(Media, "Movies", "Film", "f2.mkv"), []byte("1234567890"), 0o644)
	s2 := srv.system()
	lib2 := s2["library"].(map[string]any)
	if lib2["films"].(int64) != 5 {
		t.Fatalf("second /api/system walked disk again: films = %v, want cached 5", lib2["films"])
	}
}
