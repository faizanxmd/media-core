package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeadRoutesAndFieldsRemoved(t *testing.T) {
	cat := &Catalog{
		movies: []*Item{{ID: "m1", Type: "movie", Title: "Film", file: "/lib/Movies/Film/Film.mkv", Info: &MInfo{VCodec: "H.264", Res: "1080p", Container: "mkv", ACodec: "flac"}}},
		items:  map[string]*Item{},
	}
	srv := &Server{
		e: testEngine(t),
		p: &Player{c: cat, w: newTestWatch(), t: &TMDB{}},
	}
	h := srv.routes()

	for _, tc := range []struct {
		method, path, body string
	}{
		{"POST", "/api/manager/mp4", `{"id":"m1"}`},
		{"GET", "/api/v1/match/m1", ""},
		{"POST", "/api/v1/match", `{"id":"m1","tmdb":603}`},
		{"GET", "/api/settings", ""},
		{"GET", "/api/status", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: got status %d, want 404 or 405", tc.method, tc.path, rec.Code)
		}
	}

	// /api/jobs remains available.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/jobs", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /api/jobs = %d", rec.Code)
	}

	// /api/manager/titles no longer emits can_mp4 on mFile.
	recT := httptest.NewRecorder()
	h.ServeHTTP(recT, httptest.NewRequest("GET", "/api/manager/titles", nil))
	if recT.Code != 200 {
		t.Fatalf("GET /api/manager/titles = %d", recT.Code)
	}
	if strings.Contains(recT.Body.String(), `"can_mp4"`) {
		t.Fatalf("can_mp4 still present in /api/manager/titles: %s", recT.Body.String())
	}
}
