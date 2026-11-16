package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPlayerHandlersWaitForReady(t *testing.T) {
	cat := &Catalog{
		items: map[string]*Item{},
		ready: make(chan struct{}),
	}
	p := &Player{c: cat, w: newTestWatch(), t: &TMDB{}}
	mux := http.NewServeMux()
	p.mount(mux)

	go func() {
		time.Sleep(60 * time.Millisecond)
		m := &Item{ID: "m1", Type: "movie", Title: "Alpha Film"}
		s := &Item{ID: "s1", Type: "show", Title: "Alpha Show"}
		cat.mu.Lock()
		cat.movies = []*Item{m}
		cat.shows = []*Item{s}
		cat.items["m1"] = m
		cat.items["s1"] = s
		cat.mu.Unlock()
		close(cat.ready)
	}()

	var wg sync.WaitGroup
	checkList := func(path, key string) {
		defer wg.Done()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 {
			t.Errorf("%s: status = %d", path, rec.Code)
			return
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Errorf("%s: decode: %v", path, err)
			return
		}
		var arr []any
		json.Unmarshal(out[key], &arr)
		if len(arr) != 1 {
			t.Errorf("%s: %s len = %d, want 1", path, key, len(arr))
		}
	}

	wg.Add(5)
	go checkList("/api/v1/home", "movies")
	go checkList("/api/v1/movies", "items")
	go checkList("/api/v1/shows", "items")
	go checkList("/api/v1/search?q=alpha%20film", "items")
	go func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/item/m1", nil))
		if rec.Code != 200 {
			t.Errorf("/api/v1/item/m1: status = %d, want 200", rec.Code)
		}
	}()
	wg.Wait()
}
