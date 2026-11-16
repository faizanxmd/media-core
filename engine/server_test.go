package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func testServer(t *testing.T, ariaHandler http.HandlerFunc) (*Server, *httptest.Server) {
	t.Helper()
	ariaSrv := httptest.NewServer(ariaHandler)
	t.Cleanup(ariaSrv.Close)
	st := &Store{jobs: map[string]*Job{}, dirty: map[string]bool{}, subs: map[chan struct{}]bool{}}
	a := &Aria2{endpoint: ariaSrv.URL, token: "tok", hc: ariaSrv.Client()}
	e := NewEngine(st, a, &settingsStore{s: Settings{MaxConcurrent: 1}})
	return &Server{e: e, p: &Player{e: e, t: &TMDB{}}, assets: map[string]*asset{}}, ariaSrv
}

func defaultAriaOK(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string `json:"method"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	w.Header().Set("Content-Type", "application/json")
	switch req.Method {
	case "aria2.addUri":
		w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"gid1"}`))
	case "aria2.getSessionInfo":
		w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"sessionId":"s1"}}`))
	case "aria2.tellActive", "aria2.tellWaiting", "aria2.tellStopped":
		w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":[]}`))
	case "aria2.getGlobalStat":
		w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"downloadSpeed":"0"}}`))
	default:
		w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"OK"}`))
	}
}

func TestPostJobsLightChecksCombinedDiskSpace(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	free, _ := disk()
	if free <= 6e9 {
		t.Skip("not enough free space on test filesystem")
	}
	// Each file alone fits under free-5e9, but their combined size exceeds free-5e9.
	each := (free - 5e9) * 3 / 4

	srv, _ := testServer(t, defaultAriaOK)
	h := srv.routes()

	body, _ := json.Marshal(map[string]any{
		"Probe": Probe{URL: "http://example.com/main.mkv", Filename: "Film.2020.mkv", Size: &each, Connections: 1},
		"Plan":  Plan{Kind: "movie", Title: "Film", Year: "2020", Ext: "mkv"},
		"Light": Probe{
			URL: "http://example.com/light.mp4", Filename: "light.mp4", Size: &each, Connections: 1,
			Plan: &Plan{Kind: "movie", Title: "Film", Ext: "mp4"},
		},
	})
	req := httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("expected 400 when combined size exceeds free space, got %d: %s", rec.Code, rec.Body.String())
	}
	if jobs := srv.e.store.List(); len(jobs) != 0 {
		t.Fatalf("combined disk failure left main job half-queued (%d jobs)", len(jobs))
	}
}

func TestPostQuickSetsJobDest(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/x-matroska")
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(200)
	}))
	defer fileSrv.Close()

	srv, _ := testServer(t, defaultAriaOK)
	h := srv.routes()

	body, _ := json.Marshal(map[string]any{
		"URL":   fileSrv.URL + "/Film.2020.mkv",
		"Title": "Film",
		"Year":  "2020",
	})
	req := httptest.NewRequest("POST", "/api/quick", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK  bool `json:"ok"`
		Job Job  `json:"job"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got %s", rec.Body.String())
	}
	wantDest := "Movies/Film (2020)/Film (2020).mkv"
	if resp.Job.Dest != wantDest {
		t.Fatalf("expected job.Dest=%q, got %q", wantDest, resp.Job.Dest)
	}
}

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
