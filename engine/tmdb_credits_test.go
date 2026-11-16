package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResp(body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

const sampleMovieJSON = `{
	"id": 603,
	"title": "The Matrix",
	"overview": "Set in the 22nd century...",
	"tagline": "Welcome to the Real World.",
	"poster_path": "/p.jpg",
	"backdrop_path": "/b.jpg",
	"vote_average": 8.2,
	"runtime": 136,
	"release_date": "1999-03-31",
	"genres": [{"name": "Action"}],
	"production_companies": [{"name": "Warner Bros."}],
	"credits": {
		"cast": [{"name": "Keanu Reeves", "character": "Neo", "profile_path": "/k.jpg"}],
		"crew": [
			{"name": "Lana Wachowski", "job": "Director"},
			{"name": "Lilly Wachowski", "job": "Director"},
			{"name": "Don Davis", "job": "Original Music Composer"},
			{"name": "Someone Else", "job": "Music"}
		]
	},
	"images": {"logos": []},
	"release_dates": {"results": []}
}`

const sampleMovieMusicFallbackJSON = `{
	"id": 999,
	"title": "Fallback Film",
	"release_date": "2020-01-01",
	"credits": {
		"cast": [],
		"crew": [
			{"name": "Jane Doe", "job": "Director"},
			{"name": "Alex Score", "job": "Music"}
		]
	},
	"images": {"logos": []},
	"release_dates": {"results": []}
}`

const sampleShowJSON = `{
	"id": 1396,
	"name": "Breaking Bad",
	"overview": "A chemistry teacher...",
	"poster_path": "/sp.jpg",
	"backdrop_path": "/sb.jpg",
	"vote_average": 8.9,
	"first_air_date": "2008-01-20",
	"status": "Ended",
	"episode_run_time": [45],
	"genres": [{"name": "Drama"}],
	"networks": [{"name": "AMC"}],
	"created_by": [{"name": "Vince Gilligan"}],
	"credits": {
		"cast": [{"name": "Bryan Cranston", "character": "Walter White", "profile_path": "/bc.jpg"}],
		"crew": [
			{"name": "Dave Porter", "job": "Original Music Composer"}
		]
	},
	"images": {"logos": []},
	"content_ratings": {"results": []}
}`

func metaJSONMap(t *testing.T, m *TMeta) map[string]any {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func stringSliceField(m map[string]any, key string) []string {
	raw, _ := m[key].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestTMDBCreditsAndCacheRefresh(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	tm := &TMDB{
		key: "test-key",
		dir: dir,
		hc: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			switch r.URL.Path {
			case "/3/movie/603":
				return jsonResp(sampleMovieJSON), nil
			case "/3/movie/999":
				return jsonResp(sampleMovieMusicFallbackJSON), nil
			case "/3/tv/1396":
				return jsonResp(sampleShowJSON), nil
			default:
				t.Fatalf("unexpected TMDB request: %s", r.URL.String())
				return nil, nil
			}
		})},
		matches: map[string]tmdbMatch{
			"Movies/The Matrix (1999)": {ID: 603, At: 100},
			"TV Shows/Breaking Bad":    {ID: 1396, At: 100},
		},
		// Seed old v2 cached entries so we verify stale refresh upgrades them once.
		movies:  map[int]*TMeta{603: {TMDB: 603, Title: "The Matrix", V: 2}},
		shows:   map[int]*TMeta{1396: {TMDB: 1396, Title: "Breaking Bad", Status: "Ended", V: 2}},
		seasons: map[string]*seasonMeta{"1396/1": {TMeta: TMeta{Title: "Season 1"}, At: 100, Episodes: map[int]*TMeta{1: {Title: "Pilot"}}}},
	}

	movItem := &Item{ID: "m1", Type: "movie", Title: "The Matrix", folder: "Movies/The Matrix (1999)"}
	snItem := &Item{ID: "sn1", Type: "season", Season: 1}
	shItem := &Item{ID: "s1", Type: "show", Title: "Breaking Bad", folder: "TV Shows/Breaking Bad", Seasons: []*Item{snItem}}
	tm.Attach([]*Item{movItem}, []*Item{shItem})

	if !tm.HasMissing([]*Item{movItem}, []*Item{shItem}) {
		t.Fatal("HasMissing returned false for v2 cached entries; expected stale refresh")
	}
	if !tm.FetchMissing([]*Item{movItem}, []*Item{shItem}) {
		t.Fatal("FetchMissing returned false; expected v2 entries to be refreshed")
	}
	if calls != 2 {
		t.Fatalf("expected 2 TMDB calls (one movie, one show), got %d", calls)
	}
	tm.Attach([]*Item{movItem}, []*Item{shItem})

	if tm.HasMissing([]*Item{movItem}, []*Item{shItem}) {
		t.Fatal("HasMissing still true after refresh")
	}

	mm := metaJSONMap(t, movItem.Meta)
	if dirs := stringSliceField(mm, "director"); len(dirs) != 2 || dirs[0] != "Lana Wachowski" || dirs[1] != "Lilly Wachowski" {
		t.Fatalf("movie director = %v", dirs)
	}
	if mus := stringSliceField(mm, "music"); len(mus) != 1 || mus[0] != "Don Davis" {
		t.Fatalf("movie music = %v (want only Original Music Composer when present)", mus)
	}

	sm := metaJSONMap(t, shItem.Meta)
	if cr := stringSliceField(sm, "creator"); len(cr) != 1 || cr[0] != "Vince Gilligan" {
		t.Fatalf("show creator = %v", cr)
	}
	if mus := stringSliceField(sm, "music"); len(mus) != 1 || mus[0] != "Dave Porter" {
		t.Fatalf("show music = %v", mus)
	}

	// Fallback to "Music" job when "Original Music Composer" is absent.
	if err := tm.fetchMovie(context.Background(), 999); err != nil {
		t.Fatal(err)
	}
	fm := metaJSONMap(t, tm.movies[999])
	if mus := stringSliceField(fm, "music"); len(mus) != 1 || mus[0] != "Alex Score" {
		t.Fatalf("fallback movie music = %v", mus)
	}
}
