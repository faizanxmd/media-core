package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCancelDeleteDoesNotDeleteRetriedDownload(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	inTellStatus := make(chan struct{}, 1)
	releaseTellStatus := make(chan struct{})
	var tellOnce sync.Once
	var mu sync.Mutex
	retriedFile := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "aria2.getSessionInfo":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"sessionId":"s1"}}`))
		case "aria2.changeGlobalOption", "aria2.forceRemove", "aria2.removeDownloadResult":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"OK"}`))
		case "aria2.tellActive", "aria2.tellWaiting", "aria2.tellStopped":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":[]}`))
		case "aria2.getGlobalStat":
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"downloadSpeed":"0"}}`))
		case "aria2.tellStatus":
			tellOnce.Do(func() {
				select {
				case inTellStatus <- struct{}{}:
				default:
				}
				select {
				case <-releaseTellStatus:
				case <-r.Context().Done():
				}
			})
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{"gid":"gid1","status":"removed","files":[]}}`))
		case "aria2.addUri":
			var opts map[string]string
			if len(req.Params) >= 3 {
				json.Unmarshal(req.Params[2], &opts)
			}
			mu.Lock()
			p := filepath.Join(opts["dir"], opts["out"])
			os.MkdirAll(opts["dir"], 0o755)
			os.WriteFile(p, []byte("retried-content"), 0o644)
			retriedFile = p
			mu.Unlock()
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":"gid2"}`))
		default:
			w.Write([]byte(`{"jsonrpc":"2.0","id":"m","result":{}}`))
		}
	}))
	defer srv.Close()

	st := &Store{jobs: map[string]*Job{}, dirty: map[string]bool{}, subs: map[chan struct{}]bool{}}
	a := &Aria2{endpoint: srv.URL, token: "tok", hc: srv.Client()}
	e := NewEngine(st, a, &settingsStore{s: Settings{MaxConcurrent: 1}})

	j := &Job{
		ID:       "jcancel1",
		State:    Downloading,
		GID:      "gid1",
		URL:      "http://example.com/film.mkv",
		Filename: "Film.2020.mkv",
		Plan:     Plan{Kind: "movie", Title: "Film", Year: "2020", Ext: "mkv"},
		Log:      []string{},
	}
	st.Add(j)
	tp, err := targetPath(*j)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(tp), 0o755)
	os.WriteFile(tp, []byte("old-partial"), 0o644)
	os.WriteFile(tp+".aria2", []byte("ctrl"), 0o644)

	if _, err := e.Action(j.ID, "cancel", "", true); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	select {
	case <-inTellStatus:
	case <-time.After(2 * time.Second):
		t.Fatal("deletePartial did not call tellStatus")
	}

	// Retry while deletePartial is still waiting in tellStatus.
	// Unblock tellStatus right after Retry starts (or let cancel abort tellStatus).
	doneRetry := make(chan error, 1)
	go func() {
		_, err := e.Action(j.ID, "retry", "", false)
		doneRetry <- err
	}()

	// Give Retry a brief moment to either cancel deletePartial or block waiting for it,
	// then unblock tellStatus so old code proceeds to delete after Retry finishes.
	time.Sleep(50 * time.Millisecond)
	close(releaseTellStatus)

	if err := <-doneRetry; err != nil {
		t.Fatalf("retry: %v", err)
	}
	// Give any unawaited deletePartial goroutine time to finish its os.Remove.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	rf := retriedFile
	mu.Unlock()
	if rf == "" || !exists(rf) {
		t.Fatalf("deletePartial deleted the retried download %q", rf)
	}
}

func TestCancelDuringPostDoesNotPlaceIntoLibrary(t *testing.T) {
	oldM, oldJ := Media, JobsDir
	Media, JobsDir = t.TempDir(), t.TempDir()
	defer func() { Media, JobsDir = oldM, oldJ }()

	e := testEngine(t)
	p := Plan{Kind: "tv", Title: "Show", Season: "1"}
	j := &Job{
		ID:         "jpost111",
		State:      Processing,
		Filename:   "Show.S01E01.mkv",
		Plan:       p,
		Downloaded: true,
		Log:        []string{},
	}
	e.store.Add(j)

	dlDir, _, final, err := jobPaths(p, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(dlDir, 0o755)
	os.WriteFile(filepath.Join(dlDir, "Show.S01E01.mkv"), []byte("video"), 0o644)

	// Cancelled context before post places files
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = e.post(ctx, j.ID)

	placedFile := filepath.Join(final, "Show - S01E01.mkv")
	if exists(placedFile) {
		t.Fatalf("cancelled post placed file into library at %q", placedFile)
	}
	if cur, _ := e.store.Get(j.ID); cur.Placed {
		t.Fatalf("cancelled post marked job as Placed=true")
	}
}
