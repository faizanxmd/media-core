package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestWeblogUsesReadJSONAndTruncatesByRunes(t *testing.T) {
	srv := &Server{e: testEngine(t), p: &Player{c: &Catalog{items: map[string]*Item{}}, w: newTestWatch(), t: &TMDB{}}}
	h := srv.routes()

	req := httptest.NewRequest("POST", "/api/weblog", bytes.NewBufferString(`{"ua":"x","events":["hi"]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("weblog without Content-Type got %d, want 415", rec.Code)
	}

	var buf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldOut)

	msg := strings.Repeat("é", 405)
	body, _ := json.Marshal(map[string]any{"ua": "x", "events": []string{msg}})
	req2 := httptest.NewRequest("POST", "/api/weblog", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 204 {
		t.Fatalf("weblog status = %d", rec2.Code)
	}
	if !utf8.ValidString(buf.String()) {
		t.Fatalf("weblog produced invalid UTF-8: %q", buf.String())
	}
}

func TestSettingsBadBodyAndWriteError(t *testing.T) {
	oldS := settingsPath
	settingsPath = filepath.Join(t.TempDir(), "missing", "settings.json")
	defer func() { settingsPath = oldS }()

	e := testEngine(t)
	e.aria = &Aria2{
		endpoint: "http://mock/jsonrpc",
		hc: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			return jsonResp(`{"jsonrpc":"2.0","id":"m","result":"OK"}`), nil
		})},
	}
	e.settings.s = Settings{MaxConcurrent: 1}
	srv := &Server{e: e, p: &Player{c: &Catalog{items: map[string]*Item{}}, w: newTestWatch(), t: &TMDB{}}}
	h := srv.routes()

	req := httptest.NewRequest("POST", "/api/settings", bytes.NewBufferString(`{"limit_mbps":"not-a-number"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("bad settings body got %d, want 400: %s", rec.Code, rec.Body.String())
	}

	var buf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldOut)
	e.settings.update(func(s *Settings) { s.MaxConcurrent = 2 })
	if !strings.Contains(buf.String(), "settings") {
		t.Fatalf("expected settings write error in log, got %q", buf.String())
	}
}

func TestWatchedWithShowAndOfflineProgress(t *testing.T) {
	sh := &Item{ID: "s1", Type: "show", Title: "Raw Show", Meta: &TMeta{Title: "Real Show", Logo: "/logo.png"}}
	ep := &Item{ID: "e1", Type: "episode", Title: "Ep 1", ShowID: "s1", Show: "Raw Show", Season: 1, Episode: 1, file: "/lib/ep1.mkv"}
	cat := &Catalog{items: map[string]*Item{"s1": sh, "e1": ep}}
	p := &Player{c: cat, w: newTestWatch(), t: &TMDB{}}
	mux := http.NewServeMux()
	p.mount(mux)

	req := httptest.NewRequest("POST", "/api/v1/watched", bytes.NewBufferString(`{"id":"e1","watched":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("watched status = %d", rec.Code)
	}
	var wout struct {
		Item Item `json:"item"`
	}
	json.Unmarshal(rec.Body.Bytes(), &wout)
	if wout.Item.Show != "Real Show" || wout.Item.Meta == nil || wout.Item.Meta.Logo != "/logo.png" {
		t.Fatalf("watched item missing withShow data: show=%q meta=%+v", wout.Item.Show, wout.Item.Meta)
	}

	playMu.Lock()
	playing = map[string]time.Time{}
	playMu.Unlock()

	req2 := httptest.NewRequest("POST", "/api/v1/progress", bytes.NewBufferString(`{"id":"e1","pos":60000,"dur":600000,"at":1700000000}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.RemoteAddr = "192.168.1.50:1234"
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("progress status = %d", rec2.Code)
	}
	if n := playingNow(); n != 0 {
		t.Fatalf("offline progress report touched playing: playingNow()=%d", n)
	}
}

func TestSpeedTestStopsWhenContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "/api/v1/speedtest?bytes=52428800", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	speedTest(rec, req)
	if rec.Body.Len() > 0 {
		t.Fatalf("speedTest wrote %d bytes after context cancelled", rec.Body.Len())
	}
}

func TestImageProxyDoesNotLeakMutexes(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "img"), 0o755)
	tm := &TMDB{dir: dir}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/img/{size}/{file}", tm.serveImage)

	imgFetchMu.Lock()
	clear(imgFetches)
	imgFetchMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "/api/v1/img/w342/abcde.jpg", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	imgFetchMu.Lock()
	n := len(imgFetches)
	imgFetchMu.Unlock()
	if n != 0 {
		t.Fatalf("imgFetches leaked %d entries after serveImage returned", n)
	}
}

func TestFetchMissingOnlyMarksChangedOnSeasonSuccess(t *testing.T) {
	dir := t.TempDir()
	tm := &TMDB{
		key: "k",
		dir: dir,
		hc: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})},
		matches: map[string]tmdbMatch{"TV Shows/Show": {ID: 100, At: 1}},
		shows:   map[int]*TMeta{100: {TMDB: 100, Title: "Show", Status: "Returning Series", V: tmetaVersion}},
		seasons: map[string]*seasonMeta{},
	}
	sh := &Item{
		ID: "s1", Type: "show", Title: "Show", folder: "TV Shows/Show",
		Seasons: []*Item{{ID: "sn1", Type: "season", Season: 1}},
	}
	tm.Attach(nil, []*Item{sh})
	if tm.FetchMissing(nil, []*Item{sh}) {
		t.Fatal("FetchMissing returned true even though fetchSeason failed")
	}
}

func TestServeAPKSendsNoStore(t *testing.T) {
	oldD := DataDir
	DataDir = t.TempDir()
	defer func() { DataDir = oldD }()
	os.MkdirAll(filepath.Join(DataDir, "app"), 0o755)
	os.WriteFile(filepath.Join(DataDir, "app", "media-core.apk"), []byte("apk"), 0o644)

	rec := httptest.NewRecorder()
	serveAPK("media-core")(rec, httptest.NewRequest("GET", "/api/v1/app.apk", nil))
	if rec.Code != 200 {
		t.Fatalf("serveAPK status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestHelloMagicDNSFromTailscale(t *testing.T) {
	magicDNSMu.Lock()
	magicDNSName = ""
	magicDNSMu.Unlock()
	defer func() {
		magicDNSMu.Lock()
		magicDNSName = ""
		magicDNSMu.Unlock()
	}()

	dir := t.TempDir()
	bin := filepath.Join(dir, "tailscale")
	os.WriteFile(bin, []byte("#!/bin/sh\necho '{\"Self\":{\"DNSName\":\"custom-host.tailnet.ts.net.\"}}'\n"), 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	p := &Player{c: &Catalog{items: map[string]*Item{}}, w: newTestWatch(), t: &TMDB{}}
	rec := httptest.NewRecorder()
	p.hello(rec, httptest.NewRequest("GET", "/api/v1/hello", nil))
	var out struct {
		MagicDNS string `json:"magicdns"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.MagicDNS != "http://custom-host.tailnet.ts.net:8790" {
		t.Fatalf("magicdns = %q, want http://custom-host.tailnet.ts.net:8790", out.MagicDNS)
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

func TestTMDB429RetryCap(t *testing.T) {
	oldDelay := tmdbRetryDelay
	tmdbRetryDelay = time.Millisecond
	defer func() { tmdbRetryDelay = oldDelay }()

	attempts := 0
	tm := &TMDB{
		key: "k",
		hc: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			attempts++
			return &http.Response{
				StatusCode: 429,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		})},
	}
	var out map[string]any
	err := tm.get(context.Background(), "/movie/1", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected 429 error, got %v", err)
	}
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4 (1 initial + 3 retries)", attempts)
	}
}

func TestMainCLIFetchStatusAndPostJSONErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/jobs":
			if r.URL.Query().Get("badjson") == "1" {
				w.Write([]byte("{bad"))
				return
			}
			w.WriteHeader(500)
		case "/api/quick":
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()

	if _, err := fetchStatus(srv.URL); err == nil {
		t.Fatal("fetchStatus succeeded on HTTP 500")
	}
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{bad-json"))
	}))
	defer badServer.Close()
	if _, err := fetchStatus(badServer.URL); err == nil {
		t.Fatal("fetchStatus succeeded on invalid JSON")
	}
	var out map[string]any
	if err := postJSON(srv.URL, "/api/quick", map[string]string{"url": "x"}, &out); err == nil {
		t.Fatal("postJSON succeeded on HTTP 400")
	}
	if err := postJSON(badServer.URL, "/api/quick", map[string]string{"url": "x"}, &out); err == nil {
		t.Fatal("postJSON succeeded on invalid JSON")
	}
}
