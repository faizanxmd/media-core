package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWebCopies(t *testing.T) {
	old := DataDir
	DataDir = t.TempDir()
	defer func() { DataDir = old }()
	video := filepath.Join(t.TempDir(), "film.mkv")
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	it := &Item{ID: "abc", Type: "movie", file: video}
	p := &Player{c: &Catalog{items: map[string]*Item{"abc": it}}}
	mux := http.NewServeMux()
	mountWebCopies(mux, p)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	state := func() string {
		var m webManifest
		json.Unmarshal(get("/api/v1/web/abc").Body.Bytes(), &m)
		return m.State
	}

	if s := state(); s != "none" {
		t.Fatalf("no copy: state %q", s)
	}
	if c := get("/api/v1/web/abc/master.m3u8").Code; c != 404 {
		t.Fatalf("no copy: playlist answered %d", c)
	}

	size, mtime, _ := fileKey(video)
	dir := webDir("abc")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "master.m3u8"), []byte("#EXTM3U"), 0o644)
	write := func(size int64) {
		os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(fmt.Sprintf(`{"source_size":%d,"source_mtime":%d}`, size, mtime)), 0o644)
	}
	write(size)
	if s := state(); s != "ready" {
		t.Fatalf("copy: state %q", s)
	}
	w := get("/api/v1/web/abc/master.m3u8")
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Fatalf("playlist: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	var list struct{ Ready map[string]bool }
	json.Unmarshal(get("/api/v1/web").Body.Bytes(), &list)
	if !list.Ready["abc"] {
		t.Fatalf("list: %v", list.Ready)
	}
	for _, bad := range []string{"/api/v1/web/abc/..%2F..%2Fsecret", "/api/v1/web/nope/master.m3u8"} {
		if c := get(bad).Code; c != 404 {
			t.Errorf("%s answered %d", bad, c)
		}
	}

	write(size + 1) // the source was replaced
	if s := state(); s != "none" {
		t.Fatalf("stale copy: state %q", s)
	}
}
