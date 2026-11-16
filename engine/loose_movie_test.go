package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLooseMoviePathAndDelete(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	movRoot := filepath.Join(Media, "Movies")
	folderDir := filepath.Join(movRoot, "Folder (2022)")
	os.MkdirAll(folderDir, 0o755)
	looseFile := filepath.Join(movRoot, "Loose (2021).mkv")
	folderFile := filepath.Join(folderDir, "Folder (2022).mkv")
	os.WriteFile(looseFile, []byte("loose"), 0o644)
	os.WriteFile(folderFile, []byte("folder"), 0o644)

	cat := &Catalog{
		movies: []*Item{
			{ID: "m1", Type: "movie", Title: "Loose", Year: 2021, file: looseFile},
			{ID: "m2", Type: "movie", Title: "Folder", Year: 2022, file: folderFile},
		},
	}
	e := testEngine(t)
	srv := &Server{e: e, p: &Player{c: cat, w: newTestWatch(), t: &TMDB{}}}
	titles := srv.titles(Caps{})
	byID := map[string]mTitle{}
	for _, mt := range titles {
		byID[mt.ID] = mt
	}
	if got := byID["m1"].Path; got != "Movies/Loose (2021).mkv" {
		t.Fatalf("loose movie Path = %q, want %q", got, "Movies/Loose (2021).mkv")
	}
	if got := byID["m2"].Path; got != "Movies/Folder (2022)" {
		t.Fatalf("folder movie Path = %q, want %q", got, "Movies/Folder (2022)")
	}
	if _, err := e.DeleteFromLibrary(byID["m1"].Path); err != nil {
		t.Fatalf("DeleteFromLibrary(%q) failed: %v", byID["m1"].Path, err)
	}
	if exists(looseFile) || !exists(movRoot) || !exists(folderFile) {
		t.Fatalf("unexpected state after delete: loose=%v root=%v folder=%v", exists(looseFile), exists(movRoot), exists(folderFile))
	}
}
