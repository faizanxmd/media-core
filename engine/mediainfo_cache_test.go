package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMediaInfoNeverWritesNull(t *testing.T) {
	oldPath := miPath
	miPath = filepath.Join(t.TempDir(), "mediainfo.json")
	miMu.Lock()
	miCache = nil
	miMu.Unlock()
	defer func() {
		miMu.Lock()
		miCache = nil
		miMu.Unlock()
		miPath = oldPath
	}()

	flushMediaInfo()
	b, err := os.ReadFile(miPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != "{}" {
		t.Fatalf("flushMediaInfo wrote %q, want {}", string(b))
	}

	miMu.Lock()
	miCache = nil
	miMu.Unlock()
	saveMediaInfo(map[string]bool{})
	b, err = os.ReadFile(miPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != "{}" {
		t.Fatalf("saveMediaInfo wrote %q, want {}", string(b))
	}
}

func TestCatalogScanPrunesAndKeepsLightAndLibraryDoesNotPrune(t *testing.T) {
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
{"format":{"bit_rate":"5000000","duration":"120.0","format_name":"matroska,webm"},"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080}]}
EOF`)

	movieDir := filepath.Join(Media, "Movies", "Film (2020)")
	lightDir := filepath.Join(movieDir, ".light")
	os.MkdirAll(lightDir, 0o755)
	os.MkdirAll(filepath.Join(Media, "TV Shows"), 0o755)
	os.MkdirAll(filepath.Join(Media, "Music"), 0o755)

	mainFile := filepath.Join(movieDir, "Film (2020).mkv")
	lightFile := filepath.Join(lightDir, "Film (2020).mp4")
	staleFile := filepath.Join(Media, "Movies", "Gone (2019)", "Gone (2019).mkv")
	os.WriteFile(mainFile, []byte("main"), 0o644)
	os.WriteFile(lightFile, []byte("light"), 0o644)

	// Pre-populate stale entry in cache.
	miMu.Lock()
	miCache = map[string]miEntry{
		staleFile: {Size: 10, Mtime: 1, V: miVersion, Info: &MInfo{Res: "1080p"}},
	}
	miMu.Unlock()

	cat := &Catalog{
		items:  map[string]*Item{},
		tmdb:   &TMDB{},
		intros: &Intros{m: map[string]*introEntry{}},
		thumbs: &Thumbs{dir: filepath.Join(DataDir, "thumbs"), counts: map[string]int{}, failed: map[string]bool{}},
		noBg:   true,
	}
	cat.scan()

	miMu.Lock()
	_, hasStale := miCache[staleFile]
	_, hasMain := miCache[mainFile]
	_, hasLight := miCache[lightFile]
	miMu.Unlock()

	if hasStale {
		t.Error("Catalog.scan did not prune deleted file from mediainfo cache")
	}
	if !hasMain || !hasLight {
		t.Fatalf("Catalog.scan missing entries: main=%v light=%v", hasMain, hasLight)
	}

	// Calling GET /api/library must NOT wipe .light cache entries.
	srv := &Server{
		e: testEngine(t),
		p: &Player{c: cat, w: newTestWatch(), t: &TMDB{}},
	}
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/library", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /api/library status %d", rec.Code)
	}

	miMu.Lock()
	_, hasMainAfter := miCache[mainFile]
	_, hasLightAfter := miCache[lightFile]
	miMu.Unlock()
	if !hasMainAfter || !hasLightAfter {
		t.Fatalf("GET /api/library wiped cache entries: main=%v light=%v", hasMainAfter, hasLightAfter)
	}
}
