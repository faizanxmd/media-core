package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIncomingPerJobIsolation(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	e := testEngine(t)
	p1 := Plan{Kind: "music", Title: "Artist One"}
	p2 := Plan{Kind: "music", Title: "Artist Two"}

	j1 := &Job{ID: "job11111", State: Processing, Filename: "song1.flac", Plan: p1, Downloaded: true, Log: []string{}}
	j2 := &Job{ID: "job22222", State: Downloading, Filename: "song2.flac", Plan: p2, Log: []string{}}
	e.store.Add(j1)
	e.store.Add(j2)

	tp1, err := targetPath(*j1)
	if err != nil {
		t.Fatal(err)
	}
	tp2, err := targetPath(*j2)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(tp1) == filepath.Dir(tp2) {
		t.Fatalf("expected distinct per-job .incoming dirs, both got %q", filepath.Dir(tp1))
	}

	if err := os.MkdirAll(filepath.Dir(tp1), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(tp2), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tp1, []byte("audio1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tp2, []byte("audio2"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := e.post(context.Background(), j1.ID); err != nil {
		t.Fatalf("post job1: %v", err)
	}
	if exists(filepath.Dir(tp1)) {
		t.Fatalf("job1 incoming dir %q was not cleaned up", filepath.Dir(tp1))
	}
	if !exists(tp2) {
		t.Fatalf("job1 cleanup deleted job2's file %q", tp2)
	}
}

func TestIncomingLegacyJobStillFinishes(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	e := testEngine(t)
	p := Plan{Kind: "tv", Title: "Show", Season: "1"}
	jOld := &Job{ID: "oldjob11", State: Processing, Filename: "Show.S01E01.mkv", Plan: p, Downloaded: true, Log: []string{}}
	jNew := &Job{ID: "newjob22", State: Downloading, Filename: "Show.S01E02.mkv", Plan: p, Log: []string{}}
	e.store.Add(jOld)
	e.store.Add(jNew)

	// Old job downloaded directly into shared .incoming/ before the per-job folder change.
	sharedDir, _, final, err := planPaths(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(sharedDir, jOld.Filename)
	if err := os.WriteFile(oldFile, []byte("video1"), 0o644); err != nil {
		t.Fatal(err)
	}

	// New job is concurrently downloading into .incoming/<newjob22>/.
	tpNew, err := targetPath(*jNew)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(tpNew), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tpNew, []byte("video2"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := e.post(context.Background(), jOld.ID); err != nil {
		t.Fatalf("post legacy job: %v", err)
	}
	wantPlaced := filepath.Join(final, "Show - S01E01.mkv")
	if !exists(wantPlaced) {
		t.Fatalf("expected legacy episode placed at %q", wantPlaced)
	}
	if exists(oldFile) {
		t.Fatalf("legacy file %q should have been moved", oldFile)
	}
	if !exists(tpNew) {
		t.Fatalf("legacy job post deleted/moved concurrent new job file %q", tpNew)
	}
}
