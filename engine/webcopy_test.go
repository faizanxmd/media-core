package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebPlaybackEndpoints(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "film.mp4")
	sub := filepath.Join(dir, "film.en.srt")
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sub, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	playable := &Item{
		ID:   "abc",
		Type: "movie",
		file: video,
		Info: &MInfo{VCodec: "H.264", Res: "1080p", Container: "mp4", ACodec: "aac"},
		Subs: []XSub{{Lang: "en", Label: "English", Mime: "application/x-subrip", file: sub}},
	}
	unplayable := &Item{ID: "nope", Type: "movie", file: video}
	p := &Player{c: &Catalog{items: map[string]*Item{"abc": playable, "nope": unplayable}}}
	mux := http.NewServeMux()
	mountWebCopies(mux, p)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}

	var m webManifest
	json.Unmarshal(get("/api/v1/web/nope").Body.Bytes(), &m)
	if m.State != "none" {
		t.Fatalf("unplayable: state %q", m.State)
	}

	m = webManifest{}
	json.Unmarshal(get("/api/v1/web/abc?can=h264,aac").Body.Bytes(), &m)
	if m.State != "ready" || m.Src != "/api/v1/stream/abc" {
		t.Fatalf("direct: %+v", m)
	}

	var list struct {
		Ready   map[string]bool
		Blocked map[string]string
	}
	json.Unmarshal(get("/api/v1/web?can=h264,aac").Body.Bytes(), &list)
	if !list.Ready["abc"] || list.Ready["nope"] {
		t.Fatalf("list: %+v", list)
	}

	w := get("/api/v1/web/abc/x0.vtt")
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), "WEBVTT") {
		t.Fatalf("sidecar vtt: %d %q", w.Code, w.Body.String())
	}
	for _, bad := range []string{"/api/v1/web/abc/..%2F..%2Fsecret", "/api/v1/web/missing/x0.vtt"} {
		if c := get(bad).Code; c != 404 {
			t.Errorf("%s answered %d", bad, c)
		}
	}
}
