package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogSpecialsStaySeasonZero(t *testing.T) {
	tmp := t.TempDir()
	oldMedia, oldMiPath := Media, miPath
	miMu.Lock()
	oldMiCache := miCache
	miCache = map[string]miEntry{}
	miMu.Unlock()
	Media = filepath.Join(tmp, "media")
	miPath = filepath.Join(tmp, "mediainfo.json")
	defer func() {
		Media, miPath = oldMedia, oldMiPath
		miMu.Lock()
		miCache = oldMiCache
		miMu.Unlock()
	}()

	// Prevent catalog.go's background fillSubs goroutine from reading DataDir after cleanup.
	subsFill.Lock()

	s1Dir := filepath.Join(Media, "TV Shows", "Show", "Season 01")
	if err := os.MkdirAll(s1Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Show - S00E01.mkv", "Show - E05.mkv"} {
		if err := os.WriteFile(filepath.Join(s1Dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	c := &Catalog{
		intros:  &Intros{m: map[string]*introEntry{}},
		thumbs:  &Thumbs{dir: filepath.Join(tmp, "thumbs"), counts: map[string]int{}, failed: map[string]bool{}},
		items:   map[string]*Item{},
		refresh: make(chan struct{}, 1),
		tmdb: &TMDB{
			matches: map[string]tmdbMatch{},
			movies:  map[int]*TMeta{},
			shows:   map[int]*TMeta{},
			seasons: map[string]*seasonMeta{},
		},
		ready: make(chan struct{}),
	}
	c.intros.run.Lock()
	c.scan()

	if len(c.shows) != 1 {
		t.Fatalf("got %d shows, want 1", len(c.shows))
	}
	got := map[[2]int]string{}
	for _, sn := range c.shows[0].Seasons {
		for _, ep := range sn.Eps {
			got[[2]int{ep.Season, ep.Episode}] = filepath.Base(ep.file)
		}
	}
	if got[[2]int{0, 1}] != "Show - S00E01.mkv" {
		t.Errorf("special missing from season 0 episode 1; got %v", got)
	}
	if got[[2]int{1, 5}] != "Show - E05.mkv" {
		t.Errorf("season-1 episode missing from season 1 episode 5; got %v", got)
	}
}

func TestPlaceSpecialsStaySeasonZero(t *testing.T) {
	tmp := t.TempDir()
	oldMedia, oldJobsDir := Media, JobsDir
	Media = filepath.Join(tmp, "media")
	JobsDir = filepath.Join(tmp, "jobs")
	defer func() {
		Media, JobsDir = oldMedia, oldJobsDir
	}()
	if err := os.MkdirAll(JobsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(tmp, "pack")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Show.S00E01.mkv", "Show.E05.mkv", "Show.S00E01.en.srt", "Show.E05.en.srt"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := &Engine{store: &Store{jobs: map[string]*Job{"j1": {ID: "j1"}}, dirty: map[string]bool{}, subs: map[chan struct{}]bool{}}}
	placed, err := place(jobCtx{e: e, id: "j1"}, Plan{Kind: "tv", Title: "Show", Season: "1"}, "", src)
	if err != nil {
		t.Fatalf("place failed: %v", err)
	}
	if len(placed) != 2 {
		t.Fatalf("placed %d videos, want 2: %v", len(placed), placed)
	}

	wantFiles := []string{
		filepath.Join(Media, "TV Shows", "Show", "Season 00", "Show - S00E01.mkv"),
		filepath.Join(Media, "TV Shows", "Show", "Season 01", "Show - S01E05.mkv"),
		filepath.Join(Media, "TV Shows", "Show", "Season 00", "Show - S00E01.en.srt"),
		filepath.Join(Media, "TV Shows", "Show", "Season 01", "Show - S01E05.en.srt"),
	}
	for _, p := range wantFiles {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected placed file %s: %v", p, err)
		}
	}
}
