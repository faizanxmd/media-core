package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlaceDoesNotOverwriteExistingFiles(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	e := testEngine(t)

	// Movie video collision
	movieDir := filepath.Join(Media, "Movies", "Film (2020)")
	os.MkdirAll(movieDir, 0o755)
	origMovie := filepath.Join(movieDir, "Film (2020).mkv")
	os.WriteFile(origMovie, []byte("original-movie"), 0o644)

	srcMovie := filepath.Join(movieDir, ".incoming", "m1")
	os.MkdirAll(srcMovie, 0o755)
	os.WriteFile(filepath.Join(srcMovie, "release.mkv"), []byte("new-movie"), 0o644)
	if _, err := place(jobCtx{e, "m1"}, Plan{Kind: "movie", Title: "Film", Year: "2020", Archive: true}, movieDir, srcMovie); err == nil {
		t.Fatal("movie place overwrote existing video without error")
	}
	if b, _ := os.ReadFile(origMovie); string(b) != "original-movie" {
		t.Fatalf("existing movie was overwritten: %q", string(b))
	}

	// TV episode collision
	tvDir := filepath.Join(Media, "TV Shows", "Show", "Season 01")
	os.MkdirAll(tvDir, 0o755)
	origEp := filepath.Join(tvDir, "Show - S01E01.mkv")
	os.WriteFile(origEp, []byte("original-ep"), 0o644)

	srcTV := filepath.Join(tvDir, ".incoming", "t1")
	os.MkdirAll(srcTV, 0o755)
	os.WriteFile(filepath.Join(srcTV, "Show.S01E01.mkv"), []byte("new-ep"), 0o644)
	if _, err := place(jobCtx{e, "t1"}, Plan{Kind: "tv", Title: "Show", Season: "1"}, tvDir, srcTV); err == nil {
		t.Fatal("tv place overwrote existing episode without error")
	}
	if b, _ := os.ReadFile(origEp); string(b) != "original-ep" {
		t.Fatalf("existing episode was overwritten: %q", string(b))
	}

	// Music collision
	albumDir := filepath.Join(Media, "Music", "Artist", "Album")
	os.MkdirAll(albumDir, 0o755)
	origTrack := filepath.Join(albumDir, "01.flac")
	os.WriteFile(origTrack, []byte("original-track"), 0o644)

	srcMusic := filepath.Join(Media, "Music", ".incoming", "mu1", "Album")
	os.MkdirAll(srcMusic, 0o755)
	os.WriteFile(filepath.Join(srcMusic, "01.flac"), []byte("new-track"), 0o644)
	if _, err := place(jobCtx{e, "mu1"}, Plan{Kind: "music", Title: "Artist"}, filepath.Join(Media, "Music"), filepath.Dir(srcMusic)); err == nil {
		t.Fatal("music place overwrote existing track without error")
	}
	if b, _ := os.ReadFile(origTrack); string(b) != "original-track" {
		t.Fatalf("existing track was overwritten: %q", string(b))
	}
}

func TestPlaceSubtitlesGetDistinctNames(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	e := testEngine(t)

	// Movie subs: existing Film (2020).en.srt + two colliding subs in source
	movieDir := filepath.Join(Media, "Movies", "Film (2020)")
	os.MkdirAll(movieDir, 0o755)
	os.WriteFile(filepath.Join(movieDir, "Film (2020).en.srt"), []byte("existing-sub"), 0o644)

	srcMovie := filepath.Join(movieDir, ".incoming", "m2")
	os.MkdirAll(filepath.Join(srcMovie, "a"), 0o755)
	os.MkdirAll(filepath.Join(srcMovie, "b"), 0o755)
	os.WriteFile(filepath.Join(srcMovie, "Film.mkv"), []byte("vid"), 0o644)
	os.WriteFile(filepath.Join(srcMovie, "a", "x.en.srt"), []byte("sub-a"), 0o644)
	os.WriteFile(filepath.Join(srcMovie, "b", "y.en.srt"), []byte("sub-b"), 0o644)

	if _, err := place(jobCtx{e, "m2"}, Plan{Kind: "movie", Title: "Film", Year: "2020", Archive: true}, movieDir, srcMovie); err != nil {
		t.Fatalf("movie place failed: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(movieDir, "Film (2020).en.srt")); string(b) != "existing-sub" {
		t.Fatalf("existing movie sub overwritten: %q", string(b))
	}
	if !exists(filepath.Join(movieDir, "Film (2020).en.2.srt")) || !exists(filepath.Join(movieDir, "Film (2020).en.3.srt")) {
		t.Fatalf("expected .en.2.srt and .en.3.srt alongside existing sub")
	}

	// TV subs: existing Show - S01E01.en.srt + colliding sub in source
	tvDir := filepath.Join(Media, "TV Shows", "Show", "Season 01")
	os.MkdirAll(tvDir, 0o755)
	os.WriteFile(filepath.Join(tvDir, "Show - S01E01.en.srt"), []byte("existing-tv-sub"), 0o644)

	srcTV := filepath.Join(tvDir, ".incoming", "t2")
	os.MkdirAll(srcTV, 0o755)
	os.WriteFile(filepath.Join(srcTV, "Show.S01E01.mkv"), []byte("ep1"), 0o644)
	os.WriteFile(filepath.Join(srcTV, "Show.S01E01.en.srt"), []byte("new-tv-sub"), 0o644)

	if _, err := place(jobCtx{e, "t2"}, Plan{Kind: "tv", Title: "Show", Season: "1"}, tvDir, srcTV); err != nil {
		t.Fatalf("tv place failed: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(tvDir, "Show - S01E01.en.srt")); string(b) != "existing-tv-sub" {
		t.Fatalf("existing tv sub overwritten: %q", string(b))
	}
	if b, _ := os.ReadFile(filepath.Join(tvDir, "Show - S01E01.en.2.srt")); string(b) != "new-tv-sub" {
		t.Fatalf("expected new tv sub at .en.2.srt, got %q", string(b))
	}
}
