package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestResubmitLostGIDUsesContinue(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	var mu sync.Mutex
	var gotOpts map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "aria2.getSessionInfo":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"sessionId":"s2"}}`))
		case "aria2.changeGlobalOption":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"OK"}`))
		case "aria2.tellActive", "aria2.tellWaiting", "aria2.tellStopped":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":[]}`))
		case "aria2.getGlobalStat":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"downloadSpeed":"0"}}`))
		case "aria2.addUri":
			var opts map[string]string
			if len(req.Params) >= 3 {
				json.Unmarshal(req.Params[2], &opts)
			}
			mu.Lock()
			gotOpts = opts
			mu.Unlock()
			p := filepath.Join(opts["dir"], opts["out"])
			if exists(p) && opts["continue"] != "true" && opts["allow-overwrite"] != "true" {
				w.Write([]byte(`{"jsonrpc":"2.0","id":"m","error":{"code":1,"message":"file already exists"}}`))
				return
			}
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"gid-resumed"}`))
		default:
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{}}`))
		}
	}))
	defer srv.Close()

	st := &Store{jobs: map[string]*Job{}, dirty: map[string]bool{}, subs: map[chan struct{}]bool{}}
	a := &Aria2{endpoint: srv.URL, token: "tok", hc: srv.Client()}
	e := NewEngine(st, a, &settingsStore{s: Settings{MaxConcurrent: 1}})

	j := &Job{
		ID:        "jlost1",
		State:     Downloading,
		GID:       "old-gid",
		Started:   100,
		DoneBytes: 512,
		URL:       "http://example.com/film.mkv",
		Filename:  "Film.2020.mkv",
		Plan:      Plan{Kind: "movie", Title: "Film", Year: "2020", Ext: "mkv"},
		Log:       []string{},
	}
	st.Add(j)
	tp, err := targetPath(*j)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(tp), 0o755)
	os.WriteFile(tp, []byte("partial"), 0o644)
	os.WriteFile(tp+".aria2", []byte("ctrl"), 0o644)

	e.sync()

	got, _ := st.Get(j.ID)
	if got.State == Failed {
		t.Fatalf("resubmit failed when partial file existed: %s", got.Message)
	}
	if got.GID != "gid-resumed" {
		t.Fatalf("expected GID gid-resumed, got %q", got.GID)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotOpts["continue"] != "true" {
		t.Fatalf("expected continue=true on resubmit, got %v", gotOpts)
	}
	if gotOpts["auto-file-renaming"] != "false" {
		t.Fatalf("expected auto-file-renaming=false on resubmit, got %v", gotOpts)
	}
}

func TestFreshJobDoesNotAllowOverwrite(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	var gotOpts map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "aria2.addUri" && len(req.Params) >= 3 {
			json.Unmarshal(req.Params[2], &gotOpts)
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","error":{"code":1,"message":"file already exists"}}`))
			return
		}
		w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"OK"}`))
	}))
	defer srv.Close()

	st := &Store{jobs: map[string]*Job{}, dirty: map[string]bool{}, subs: map[chan struct{}]bool{}}
	a := &Aria2{endpoint: srv.URL, token: "tok", hc: srv.Client()}
	e := NewEngine(st, a, &settingsStore{s: Settings{MaxConcurrent: 1}})

	dir := filepath.Join(Media, "Movies", "Film (2020)")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "Film (2020).mkv"), []byte("existing-library-file"), 0o644)

	_, _ = e.NewJob(
		Probe{URL: "http://example.com/film.mkv", Filename: "Film.2020.mkv", Connections: 1},
		Plan{Kind: "movie", Title: "Film", Year: "2020", Ext: "mkv"},
	)
	if gotOpts["allow-overwrite"] == "true" || gotOpts["continue"] == "true" {
		t.Fatalf("fresh job must not set allow-overwrite or continue onto existing library file: %v", gotOpts)
	}
}
