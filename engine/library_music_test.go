package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAPILibraryScansMusicOnly(t *testing.T) {
	oldM, oldD, oldPath := Media, DataDir, miPath
	Media = t.TempDir()
	DataDir = t.TempDir()
	miPath = filepath.Join(DataDir, "mediainfo.json")
	miMu.Lock()
	miCache = nil
	miMu.Unlock()
	defer func() {
		miMu.Lock()
		miCache = nil
		miMu.Unlock()
		Media, DataDir, miPath = oldM, oldD, oldPath
	}()

	writeFakeFFprobe(t, `cat <<'EOF'
{"format":{"bit_rate":"1000000","duration":"200.0","format_name":"flac"},"streams":[{"codec_type":"audio","codec_name":"flac","bits_per_raw_sample":"16","sample_rate":"44100"}]}
EOF`)

	os.MkdirAll(filepath.Join(Media, "Movies", "Film (2020)"), 0o755)
	os.WriteFile(filepath.Join(Media, "Movies", "Film (2020)", "Film (2020).mkv"), []byte("v"), 0o644)
	os.MkdirAll(filepath.Join(Media, "TV Shows", "Show", "Season 01"), 0o755)
	os.WriteFile(filepath.Join(Media, "TV Shows", "Show", "Season 01", "Show - S01E01.mkv"), []byte("v"), 0o644)
	albumDir := filepath.Join(Media, "Music", "Artist", "Album")
	os.MkdirAll(albumDir, 0o755)
	os.WriteFile(filepath.Join(albumDir, "01.flac"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(albumDir, "cover.jpg"), []byte("img"), 0o644)

	cat := &Catalog{items: map[string]*Item{}}
	srv := &Server{
		e: testEngine(t),
		p: &Player{c: cat, w: newTestWatch(), t: &TMDB{}},
	}
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/library", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["Music"]; !ok {
		t.Fatalf("missing Music key: %v", got)
	}
	if _, ok := got["Movies"]; ok {
		t.Fatalf("unexpected Movies key in /api/library response")
	}
	if _, ok := got["TV Shows"]; ok {
		t.Fatalf("unexpected TV Shows key in /api/library response")
	}
}
