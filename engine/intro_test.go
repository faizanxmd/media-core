package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestChapterCredits(t *testing.T) {
	// Opening credits at the start must not count; real end credits near the end must count.
	chs := []Chapter{
		{Start: 0, End: 90, Title: "Opening Credits"},
		{Start: 90, End: 1200, Title: "Main"},
		{Start: 1200, End: 1320, Title: "End Credits"},
	}
	got := chapterCredits(chs)
	if got == nil || got.Start != 1200 || got.End != 1320 {
		t.Fatalf("got %+v, want {1200 1320}", got)
	}

	// Only an "Opening Credits" chapter -> nil.
	if got := chapterCredits([]Chapter{{Start: 0, End: 90, Title: "Opening Credits"}, {Start: 90, End: 1300, Title: "Episode"}}); got != nil {
		t.Fatalf("expected nil for Opening Credits only, got %+v", got)
	}

	// A chapter titled "Credits" starting in the first 25% of runtime must not count.
	if got := chapterCredits([]Chapter{{Start: 60, End: 120, Title: "Credits"}, {Start: 120, End: 1000, Title: "Part 1"}}); got != nil {
		t.Fatalf("expected nil for early Credits (<25%%), got %+v", got)
	}

	// An "Opening Credits" chapter even past 25% must not count.
	if got := chapterCredits([]Chapter{{Start: 400, End: 480, Title: "Opening Credits"}, {Start: 480, End: 1000, Title: "Part 2"}}); got != nil {
		t.Fatalf("expected nil for Opening Credits past 25%%, got %+v", got)
	}
}

func TestIntrosFailureRecordedAndPruned(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	oldData, oldMedia, oldIntros := DataDir, Media, introsPath
	DataDir = t.TempDir()
	Media = t.TempDir()
	introsPath = filepath.Join(DataDir, "intros.json")
	defer func() { DataDir, Media, introsPath = oldData, oldMedia, oldIntros }()

	epDir := filepath.Join(Media, "TV Shows", "Show", "Season 01")
	os.MkdirAll(epDir, 0o755)
	ep1 := filepath.Join(epDir, "Show - S01E01.mkv")
	ep2 := filepath.Join(epDir, "Show - S01E02.mkv")
	// Corrupt/empty video files so ffmpeg chromaprint fails.
	os.WriteFile(ep1, []byte("not a valid media file"), 0o644)
	os.WriteFile(ep2, []byte("not a valid media file either"), 0o644)

	// Unsettled files (< thumbSettle): Pending must not claim work or record failure yet.
	in := NewIntros()
	shows := []*Item{{
		Type: "show", Title: "Show",
		Seasons: []*Item{{
			Type: "season", Title: "Season 01",
			Eps: []*Item{{file: ep1}, {file: ep2}},
		}},
	}}
	if fp := fingerprint(ep1); fp != nil {
		t.Fatalf("expected nil fingerprint for unsettled file")
	}

	// Settle both files (> thumbSettle).
	old := time.Now().Add(-time.Hour)
	os.Chtimes(ep1, old, old)
	os.Chtimes(ep2, old, old)

	if !in.Pending(shows) {
		t.Fatal("expected Pending=true for settled unknown episodes")
	}
	in.Analyse(shows)

	// After failed fingerprint on settled files, Pending must be false (no infinite retry).
	if in.Pending(shows) {
		t.Fatal("Pending should be false after recording fingerprint failures")
	}

	// When size/mtime changes, Pending should become true again so it retries.
	os.WriteFile(ep1, []byte("replaced file with different size"), 0o644)
	os.Chtimes(ep1, old, old)
	if !in.Pending(shows) {
		t.Fatal("expected Pending=true after ep1 size changed")
	}

	// Test pruning of deleted files from intros.json and fp/*.fp.
	staleEp := filepath.Join(epDir, "Show - S01E99.mkv")
	staleFP := filepath.Join(DataDir, "fp", pathID(rel(staleEp))+".fp")
	keepFP := filepath.Join(DataDir, "fp", pathID(rel(ep1))+".fp")
	os.WriteFile(staleFP, []byte("stale"), 0o644)
	os.WriteFile(keepFP, []byte("keep"), 0o644)
	in.mu.Lock()
	in.m[staleEp] = &introEntry{Size: 10, Mtime: 10, V: introVersion}
	writeJSONFile(in.path, in.m)
	in.mu.Unlock()

	in.Pending(shows)

	in.mu.Lock()
	_, hasStale := in.m[staleEp]
	in.mu.Unlock()
	if hasStale {
		t.Fatal("stale episode was not pruned from intros.json")
	}
	if exists(staleFP) {
		t.Fatal("stale fp file was not pruned")
	}
	if !exists(keepFP) {
		t.Fatal("live fp file was erroneously pruned")
	}
}
