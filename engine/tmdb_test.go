package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTitleSim(t *testing.T) {
	for _, c := range []struct {
		a, b string
		ok   bool
	}{
		{"Oldboy", "Oldboy", true},
		{"A Clockwork Orange", "Clockwork Orange", true},
		{"True Detective", "True Detective", true},
		{"Mint Test", "Les Gorilles", false},
		{"The Office", "The Office (US)", true},
		{"Spider-Man: No Way Home", "Spider-Man No Way Home", true},
		{"Dune", "Dune: Part Two", false},
	} {
		if got := titleSim(c.a, c.b) >= 0.6; got != c.ok {
			t.Errorf("titleSim(%q, %q) = %.2f, want ok=%v", c.a, c.b, titleSim(c.a, c.b), c.ok)
		}
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
