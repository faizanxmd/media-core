package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TMeta is what TMDB tells us about a film, show, season or episode.
type TMeta struct {
	TMDB     int      `json:"tmdb,omitempty"`
	Title    string   `json:"title,omitempty"`
	Year     int      `json:"year,omitempty"`
	Overview string   `json:"overview,omitempty"`
	Tagline  string   `json:"tagline,omitempty"`
	Poster   string   `json:"poster,omitempty"` // TMDB image file, e.g. "/abc.jpg"; fetch via /api/v1/img/<size>/abc.jpg
	Backdrop string   `json:"backdrop,omitempty"`
	Logo     string   `json:"logo,omitempty"`
	Still    string   `json:"still,omitempty"`
	Rating   float64  `json:"rating,omitempty"`
	Genres   []string `json:"genres,omitempty"`
	Cast     []Person `json:"cast,omitempty"`
	Director []string `json:"director,omitempty"` // films: who directed it
	Music    []string `json:"music,omitempty"`    // films and shows: the composer
	Creator  []string `json:"creator,omitempty"`  // shows: who created it
	Runtime  int      `json:"runtime,omitempty"`  // minutes
	Cert     string   `json:"cert,omitempty"`     // U/A 16+, R, TV-MA…
	Date     string   `json:"date,omitempty"`     // release / first air date
	Studio   string   `json:"studio,omitempty"`
	Status   string   `json:"status,omitempty"` // shows: Returning Series, Ended
	V        int      `json:"v,omitempty"`      // tmetaVersion it was fetched with
}

// Bump when what fetchMovie/fetchShow store changes, so cached entries are re-read.
const tmetaVersion = 5 // past both branches' caches (3 and 4), so every title refreshes once

type Person struct {
	Name  string `json:"name"`
	Role  string `json:"role,omitempty"`
	Photo string `json:"photo,omitempty"`
}

type tmdbMatch struct {
	ID int     `json:"id"` // 0 = searched, nothing found
	At float64 `json:"at"`
}

// seasonMeta is a season plus its episodes, cached per season.
type seasonMeta struct {
	TMeta
	At       float64        `json:"at"`
	Failed   float64        `json:"failed,omitempty"` // last failed refresh: wait before trying again
	Wait     float64        `json:"wait,omitempty"`   // how long, in seconds (0 means an hour)
	Episodes map[int]*TMeta `json:"episodes"`
}

type TMDB struct {
	key     string
	dir     string
	hc      *http.Client
	mu      sync.Mutex
	matches map[string]tmdbMatch // movie/show folder (relative to Media) -> TMDB id
	movies  map[int]*TMeta
	shows   map[int]*TMeta
	seasons map[string]*seasonMeta // "<show id>/<season>"
	fetchMu sync.Mutex
}

var tmdbDir = filepath.Join(DataDir, "tmdb")

func NewTMDB() *TMDB {
	t := &TMDB{dir: tmdbDir, hc: &http.Client{Timeout: 20 * time.Second}, matches: map[string]tmdbMatch{},
		movies: map[int]*TMeta{}, shows: map[int]*TMeta{}, seasons: map[string]*seasonMeta{}}
	if b, err := os.ReadFile(filepath.Join(ConfDir, "tmdb.key")); err == nil {
		t.key = strings.TrimSpace(string(b))
	}
	os.MkdirAll(filepath.Join(t.dir, "img"), 0o755)
	readJSONFile(filepath.Join(t.dir, "matches.json"), &t.matches)
	readJSONFile(filepath.Join(t.dir, "movies.json"), &t.movies)
	readJSONFile(filepath.Join(t.dir, "shows.json"), &t.shows)
	readJSONFile(filepath.Join(t.dir, "seasons.json"), &t.seasons)
	if t.matches == nil {
		t.matches = map[string]tmdbMatch{}
	}
	if t.movies == nil {
		t.movies = map[int]*TMeta{}
	}
	if t.shows == nil {
		t.shows = map[int]*TMeta{}
	}
	if t.seasons == nil {
		t.seasons = map[string]*seasonMeta{}
	}
	return t
}

func readJSONFile(p string, v any) {
	if b, err := os.ReadFile(p); err == nil {
		json.Unmarshal(b, v)
	}
}

func writeJSONFile(p string, v any) {
	b, _ := json.Marshal(v)
	if os.WriteFile(p+".tmp", b, 0o644) == nil {
		os.Rename(p+".tmp", p)
	}
}

func (t *TMDB) save() {
	writeJSONFile(filepath.Join(t.dir, "matches.json"), t.matches)
	writeJSONFile(filepath.Join(t.dir, "movies.json"), t.movies)
	writeJSONFile(filepath.Join(t.dir, "shows.json"), t.shows)
	writeJSONFile(filepath.Join(t.dir, "seasons.json"), t.seasons)
}

var tmdbRetryDelay = 2 * time.Second

func (t *TMDB) get(ctx context.Context, p string, q url.Values, out any) error {
	if t.key == "" {
		return errors.New("no TMDB key in ~/.config/media-core/tmdb.key")
	}
	if q == nil {
		q = url.Values{}
	} else {
		q = qCopy(q)
	}
	if len(t.key) <= 40 {
		q.Set("api_key", t.key)
	}
	rawURL := "https://api.themoviedb.org/3" + p + "?" + q.Encode()
	for attempt := 0; ; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
		if len(t.key) > 40 { // v4 read access token
			req.Header.Set("Authorization", "Bearer "+t.key)
		}
		resp, err := t.hc.Do(req)
		if err != nil {
			var ue *url.Error
			if errors.As(err, &ue) {
				return ue.Err // the URL holds the API key: keep it out of the logs
			}
			return err
		}
		if resp.StatusCode == 429 && attempt < 3 {
			resp.Body.Close()
			tm := time.NewTimer(tmdbRetryDelay)
			select {
			case <-ctx.Done():
				tm.Stop()
				return ctx.Err()
			case <-tm.C:
			}
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("TMDB %s: HTTP %d", p, resp.StatusCode)
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
}

func qCopy(q url.Values) url.Values {
	out := make(url.Values, len(q))
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// Attach hangs cached metadata on the catalog's items. It never touches the
// network, so a scan is instant; FetchMissing fills the gaps afterwards.
func (t *TMDB) Attach(movies, shows []*Item) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, m := range movies {
		if mt, ok := t.matches[m.folder]; ok && mt.ID > 0 {
			m.Meta = t.movies[mt.ID]
		}
	}
	for _, s := range shows {
		mt, ok := t.matches[s.folder]
		if !ok || mt.ID == 0 {
			continue
		}
		s.Meta = t.shows[mt.ID]
		// TMDB's show poster is the newest season's ("Night Country" for True
		// Detective); with only one season on disk, show that season's poster.
		if s.Meta != nil && len(s.Seasons) == 1 {
			if sm := t.seasons[fmt.Sprintf("%d/%d", mt.ID, s.Seasons[0].Season)]; sm != nil && sm.Poster != "" {
				m := *s.Meta
				m.Poster = sm.Poster
				s.Meta = &m
			}
		}
		for _, sn := range s.Seasons {
			sm := t.seasons[fmt.Sprintf("%d/%d", mt.ID, sn.Season)]
			if sm == nil {
				continue
			}
			meta := sm.TMeta
			sn.Meta = &meta
			for _, ep := range sn.Eps {
				if em := sm.Episodes[ep.Episode]; em != nil {
					ep.Meta = em
					if em.Title != "" {
						ep.Title = em.Title
					}
				}
			}
		}
	}
}

// HasMissing reports whether any folder still needs a lookup.
func (t *TMDB) HasMissing(movies, shows []*Item) bool {
	if t.key == "" {
		return false
	}
	for _, m := range movies {
		if m.Meta == nil && t.retryable(m.folder) || t.stale(t.movies, m.folder) > 0 {
			return true
		}
	}
	for _, s := range shows {
		if s.Meta == nil && t.retryable(s.folder) || t.stale(t.shows, s.folder) > 0 {
			return true
		}
		if s.Meta != nil {
			for _, sn := range s.Seasons {
				if sn.Meta == nil || t.seasonStale(s.Meta.TMDB, sn.Season, s.Meta.Status) {
					return true
				}
			}
		}
	}
	return false
}

// retryable: never looked up, or a failed lookup more than a day ago.
func (t *TMDB) retryable(folder string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	mt, ok := t.matches[folder]
	return !ok || mt.ID == 0 && now()-mt.At > 86400
}

// stale returns the matched TMDB id when its cached details are missing or
// from an older tmetaVersion (0 otherwise).
func (t *TMDB) stale(cache map[int]*TMeta, folder string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	mt, ok := t.matches[folder]
	if !ok || mt.ID == 0 {
		return 0
	}
	if m := cache[mt.ID]; m == nil || m.V != tmetaVersion {
		return mt.ID
	}
	return 0
}

// seasonStale: a running show's season is re-read every 3 days for new
// episode titles; ended shows never. A season we hold nothing for is always
// wanted. After a failed fetch, wait before trying again: a few minutes when
// the network dropped, an hour when TMDB answered with an error.
func (t *TMDB) seasonStale(show, season int, status string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	sm := t.seasons[fmt.Sprintf("%d/%d", show, season)]
	if sm == nil {
		return true
	}
	wait := sm.Wait
	if wait == 0 {
		wait = 3600
	}
	if now()-sm.Failed < wait {
		return false
	}
	return len(sm.Episodes) == 0 || status != "Ended" && status != "Canceled" && now()-sm.At > 3*86400
}

// FetchMissing looks up everything not cached yet. Returns true when it
// learned something (so the catalog re-attaches).
func (t *TMDB) FetchMissing(movies, shows []*Item) bool {
	if !t.fetchMu.TryLock() {
		return false // another pass is already running
	}
	defer t.fetchMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	changed := false
	for _, m := range movies {
		if id := t.stale(t.movies, m.folder); id > 0 {
			if err := t.fetchMovie(ctx, id); err != nil {
				log.Printf("tmdb: movie %d: %v", id, err)
			} else {
				changed = true
			}
			continue
		}
		if m.Meta != nil || !t.retryable(m.folder) {
			continue
		}
		id, err := t.search(ctx, "movie", m.Title, m.Year)
		if err != nil {
			log.Printf("tmdb: %s: %v", m.folder, err)
			continue
		}
		if id > 0 {
			if err := t.fetchMovie(ctx, id); err != nil {
				log.Printf("tmdb: movie %d: %v", id, err)
				continue
			}
		}
		t.setMatch(m.folder, id)
		changed = true
	}
	for _, s := range shows {
		id := 0
		if sid := t.stale(t.shows, s.folder); sid > 0 {
			if err := t.fetchShow(ctx, sid); err != nil {
				log.Printf("tmdb: show %d: %v", sid, err)
				continue
			}
			id, changed = sid, true
		} else if s.Meta != nil {
			id = s.Meta.TMDB
		} else if t.retryable(s.folder) {
			var err error
			if id, err = t.search(ctx, "tv", s.Title, s.Year); err != nil {
				log.Printf("tmdb: %s: %v", s.folder, err)
				continue
			}
			if id > 0 {
				if err := t.fetchShow(ctx, id); err != nil {
					log.Printf("tmdb: show %d: %v", id, err)
					continue
				}
			}
			t.setMatch(s.folder, id)
			changed = true
		}
		if id == 0 {
			continue
		}
		t.mu.Lock()
		status := ""
		if sm := t.shows[id]; sm != nil {
			status = sm.Status
		}
		t.mu.Unlock()
		for _, sn := range s.Seasons {
			if sn.Meta != nil && !t.seasonStale(id, sn.Season, status) {
				continue
			}
			if err := t.fetchSeason(ctx, id, sn.Season); err != nil {
				log.Printf("tmdb: show %d season %d: %v", id, sn.Season, err)
				t.mu.Lock() // keep what we already have: one network error must not wipe a season
				key := fmt.Sprintf("%d/%d", id, sn.Season)
				sm := t.seasons[key]
				if sm == nil {
					sm = &seasonMeta{At: now(), Episodes: map[int]*TMeta{}}
					t.seasons[key] = sm
				}
				sm.Failed, sm.Wait = now(), 3600
				if !strings.HasPrefix(err.Error(), "TMDB ") { // no answer at all (reset, timeout): try again soon
					sm.Wait = 180
				}
				t.mu.Unlock()
			} else {
				changed = true
			}
		}
	}
	if changed {
		t.mu.Lock()
		t.save()
		t.mu.Unlock()
	}
	return changed
}

func (t *TMDB) setMatch(folder string, id int) {
	t.mu.Lock()
	t.matches[folder] = tmdbMatch{ID: id, At: now()}
	t.mu.Unlock()
}

type tmdbResult struct {
	ID       int     `json:"id"`
	Title    string  `json:"title"`
	Name     string  `json:"name"`
	OrigT    string  `json:"original_title"`
	OrigN    string  `json:"original_name"`
	Date     string  `json:"release_date"`
	AirDate  string  `json:"first_air_date"`
	Poster   string  `json:"poster_path"`
	Overview string  `json:"overview"`
	Pop      float64 `json:"popularity"`
}

func (r tmdbResult) year() int {
	d := r.Date + r.AirDate
	if len(d) >= 4 {
		y, _ := strconv.Atoi(d[:4])
		return y
	}
	return 0
}

// Search is used by the app's "fix match" screen as well.
func (t *TMDB) Search(ctx context.Context, kind, title string, year int) ([]tmdbResult, error) {
	q := url.Values{"query": {title}, "include_adult": {"false"}}
	if year > 0 {
		if kind == "movie" {
			q.Set("year", strconv.Itoa(year))
		} else {
			q.Set("first_air_date_year", strconv.Itoa(year))
		}
	}
	var r struct{ Results []tmdbResult }
	if err := t.get(ctx, "/search/"+kind, q, &r); err != nil {
		return nil, err
	}
	return r.Results, nil
}

var reNonWord = regexp.MustCompile(`[^\pL\pN]+`)
var reParens = regexp.MustCompile(`\([^)]*\)`)

func normTitle(s string) string {
	return strings.Trim(reNonWord.ReplaceAllString(strings.ToLower(s), " "), " ")
}

func (t *TMDB) search(ctx context.Context, kind, title string, year int) (int, error) {
	res, err := t.Search(ctx, kind, title, year)
	if err == nil && len(res) == 0 && year > 0 { // year off by one in the file name, or none on TMDB
		res, err = t.Search(ctx, kind, title, 0)
	}
	if err != nil || len(res) == 0 {
		return 0, err
	}
	// TMDB's search is fuzzy and always returns *something*; only accept a
	// result whose title really matches (a wrong poster is worse than none:
	// the app can fix an unmatched item by hand).
	bestID, bestScore := 0, 0.0
	for i, r := range res {
		sim := max(titleSim(title, r.Title+r.Name), titleSim(title, r.OrigT+r.OrigN))
		if sim < 0.6 {
			continue
		}
		score := sim - float64(i)*0.01 // TMDB's order breaks ties
		if year > 0 && r.year() > 0 {
			switch d := abs(r.year() - year); {
			case d == 0:
				score += 0.5
			case d == 1:
				score += 0.3
			default:
				score -= 0.3
			}
		}
		if score > bestScore {
			bestID, bestScore = r.ID, score
		}
	}
	return bestID, nil
}

// titleSim is the share of words two titles have in common (0..1),
// ignoring case, punctuation and "the/a/an".
func titleSim(a, b string) float64 {
	words := func(s string) map[string]bool {
		m := map[string]bool{}
		for _, w := range strings.Fields(normTitle(reParens.ReplaceAllString(s, " "))) { // "The Office (US)"
			if w != "the" && w != "a" && w != "an" && w != "and" {
				m[w] = true
			}
		}
		return m
	}
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	common := 0
	for w := range wa {
		if wb[w] {
			common++
		}
	}
	return float64(common) / float64(max(len(wa), len(wb)))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

type tmdbImages struct {
	Logos []struct {
		Path string  `json:"file_path"`
		Lang *string `json:"iso_639_1"`
		Vote float64 `json:"vote_average"`
	}
}

// logo picks an English (or language-less) logo, preferring PNG.
// logo picks the title's logo: English first, then one without words, then one in the title's own language
// (TMDB often tags an Indian film's Latin-letter logo with its language, not English).
func (im tmdbImages) logo(orig string) string {
	best, bestScore := "", -1.0
	for _, l := range im.Logos {
		score := l.Vote
		switch {
		case l.Lang != nil && *l.Lang == "en":
			score += 30
		case l.Lang == nil:
			score += 20
		case orig != "" && orig != "en" && l.Lang != nil && *l.Lang == orig:
			score += 10
		default:
			continue
		}
		if strings.HasSuffix(l.Path, ".svg") {
			continue // Android can't decode SVG without extra libraries
		}
		if score > bestScore {
			best, bestScore = l.Path, score
		}
	}
	return best
}

// ownLogo asks again for a logo in the title's own language, when there's none in English or without words.
func (t *TMDB) ownLogo(ctx context.Context, path, orig string) string {
	if orig == "" || orig == "en" {
		return ""
	}
	var im tmdbImages
	if t.get(ctx, path+"/images", url.Values{"include_image_language": {orig}}, &im) != nil {
		return ""
	}
	return im.logo(orig)
}

type tmdbCredits struct {
	Cast []struct {
		Name      string `json:"name"`
		Character string `json:"character"`
		Profile   string `json:"profile_path"`
	}
	Crew []struct {
		Name string `json:"name"`
		Job  string `json:"job"`
	}
}

func (c tmdbCredits) people(n int) []Person {
	var out []Person
	for i, p := range c.Cast {
		if i >= n {
			break
		}
		out = append(out, Person{p.Name, p.Character, p.Profile})
	}
	return out
}

func (c tmdbCredits) crewByJob(job string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range c.Crew {
		if p.Job == job && p.Name != "" && !seen[p.Name] {
			seen[p.Name] = true
			out = append(out, p.Name)
		}
	}
	return out
}

func (c tmdbCredits) music() []string {
	if m := c.crewByJob("Original Music Composer"); len(m) > 0 {
		return m
	}
	return c.crewByJob("Music")
}

type tmdbAggCredits struct {
	Crew []struct {
		Name string `json:"name"`
		Jobs []struct {
			Job string `json:"job"`
		} `json:"jobs"`
	} `json:"crew"`
}

func (c tmdbAggCredits) music() []string {
	for _, want := range []string{"Original Music Composer", "Music"} {
		var out []string
		seen := map[string]bool{}
		for _, p := range c.Crew {
			if p.Name == "" || seen[p.Name] {
				continue
			}
			for _, j := range p.Jobs {
				if j.Job == want {
					seen[p.Name] = true
					out = append(out, p.Name)
					break
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

type namedList []struct{ Name string }

func (l namedList) names() []string {
	var out []string
	seen := map[string]bool{}
	for _, g := range l {
		if g.Name != "" && !seen[g.Name] {
			seen[g.Name] = true
			out = append(out, g.Name)
		}
	}
	return out
}

// Certifications: US first (TMDB's Indian ratings are patchy), then India.
var certCountries = []string{"US", "IN", "GB"}

func (t *TMDB) fetchMovie(ctx context.Context, id int) error {
	var r struct {
		ID       int       `json:"id"`
		Title    string    `json:"title"`
		Overview string    `json:"overview"`
		Tagline  string    `json:"tagline"`
		Poster   string    `json:"poster_path"`
		Backdrop string    `json:"backdrop_path"`
		Vote     float64   `json:"vote_average"`
		Runtime  int       `json:"runtime"`
		Date     string    `json:"release_date"`
		Genres   namedList `json:"genres"`
		Studios  namedList `json:"production_companies"`
		Credits  tmdbCredits
		Images   tmdbImages
		Orig     string `json:"original_language"`
		Release  struct {
			Results []struct {
				Country string `json:"iso_3166_1"`
				Dates   []struct {
					Cert string `json:"certification"`
				} `json:"release_dates"`
			}
		} `json:"release_dates"`
	}
	q := url.Values{"append_to_response": {"credits,images,release_dates"}, "include_image_language": {"en,null"}}
	if err := t.get(ctx, "/movie/"+strconv.Itoa(id), q, &r); err != nil {
		return err
	}
	m := &TMeta{TMDB: r.ID, Title: r.Title, Overview: r.Overview, Tagline: r.Tagline, Poster: r.Poster,
		Backdrop: r.Backdrop, Logo: r.Images.logo(r.Orig), Rating: round1(r.Vote), Genres: r.Genres.names(),
		Cast: r.Credits.people(12), Director: r.Credits.crewByJob("Director"), Music: r.Credits.music(),
		Runtime: r.Runtime, Date: r.Date, Year: yearOf(r.Date), V: tmetaVersion}
	if m.Logo == "" {
		m.Logo = t.ownLogo(ctx, "/movie/"+strconv.Itoa(id), r.Orig)
	}
	if s := r.Studios.names(); len(s) > 0 {
		m.Studio = s[0]
	}
cert:
	for _, c := range certCountries {
		for _, res := range r.Release.Results {
			if res.Country != c {
				continue
			}
			for _, d := range res.Dates {
				if d.Cert != "" {
					m.Cert = d.Cert
					break cert
				}
			}
		}
	}
	t.mu.Lock()
	t.movies[id] = m
	t.mu.Unlock()
	return nil
}

func (t *TMDB) fetchShow(ctx context.Context, id int) error {
	var r struct {
		ID               int       `json:"id"`
		Name             string    `json:"name"`
		Overview         string    `json:"overview"`
		Tagline          string    `json:"tagline"`
		Poster           string    `json:"poster_path"`
		Backdrop         string    `json:"backdrop_path"`
		Vote             float64   `json:"vote_average"`
		Date             string    `json:"first_air_date"`
		Status           string    `json:"status"`
		Runtime          []int     `json:"episode_run_time"`
		Genres           namedList `json:"genres"`
		Networks         namedList `json:"networks"`
		CreatedBy        namedList `json:"created_by"`
		Credits          tmdbCredits
		AggregateCredits tmdbAggCredits `json:"aggregate_credits"`
		Images           tmdbImages
		Orig             string `json:"original_language"`
		Ratings          struct {
			Results []struct {
				Country string `json:"iso_3166_1"`
				Rating  string `json:"rating"`
			}
		} `json:"content_ratings"`
	}
	q := url.Values{"append_to_response": {"credits,aggregate_credits,images,content_ratings"}, "include_image_language": {"en,null"}}
	if err := t.get(ctx, "/tv/"+strconv.Itoa(id), q, &r); err != nil {
		return err
	}
	music := r.Credits.music()
	if len(music) == 0 {
		music = r.AggregateCredits.music()
	}
	m := &TMeta{TMDB: r.ID, Title: r.Name, Overview: r.Overview, Tagline: r.Tagline, Poster: r.Poster,
		Backdrop: r.Backdrop, Logo: r.Images.logo(r.Orig), Rating: round1(r.Vote), Genres: r.Genres.names(),
		Cast: r.Credits.people(12), Creator: r.CreatedBy.names(), Music: music,
		Date: r.Date, Year: yearOf(r.Date), Status: r.Status, V: tmetaVersion}
	if m.Logo == "" {
		m.Logo = t.ownLogo(ctx, "/tv/"+strconv.Itoa(id), r.Orig)
	}
	if len(r.Runtime) > 0 {
		m.Runtime = r.Runtime[0]
	}
	if n := r.Networks.names(); len(n) > 0 {
		m.Studio = n[0]
	}
cert:
	for _, c := range certCountries {
		for _, res := range r.Ratings.Results {
			if res.Country == c && res.Rating != "" {
				m.Cert = res.Rating
				break cert
			}
		}
	}
	t.mu.Lock()
	t.shows[id] = m
	t.mu.Unlock()
	return nil
}

func (t *TMDB) fetchSeason(ctx context.Context, show, season int) error {
	var r struct {
		ID       int     `json:"id"`
		Name     string  `json:"name"`
		Overview string  `json:"overview"`
		Poster   string  `json:"poster_path"`
		Date     string  `json:"air_date"`
		Vote     float64 `json:"vote_average"`
		Episodes []struct {
			Number   int     `json:"episode_number"`
			Name     string  `json:"name"`
			Overview string  `json:"overview"`
			Still    string  `json:"still_path"`
			Date     string  `json:"air_date"`
			Runtime  int     `json:"runtime"`
			Vote     float64 `json:"vote_average"`
		}
	}
	if err := t.get(ctx, fmt.Sprintf("/tv/%d/season/%d", show, season), nil, &r); err != nil {
		return err
	}
	sm := &seasonMeta{TMeta: TMeta{TMDB: r.ID, Title: r.Name, Overview: r.Overview, Poster: r.Poster, Date: r.Date,
		Year: yearOf(r.Date), Rating: round1(r.Vote)}, At: now(), Episodes: map[int]*TMeta{}}
	for _, e := range r.Episodes {
		sm.Episodes[e.Number] = &TMeta{Title: e.Name, Overview: e.Overview, Still: e.Still, Date: e.Date,
			Runtime: e.Runtime, Rating: round1(e.Vote)}
	}
	t.mu.Lock()
	t.seasons[fmt.Sprintf("%d/%d", show, season)] = sm
	t.mu.Unlock()
	return nil
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

func yearOf(date string) int {
	if len(date) >= 4 {
		y, _ := strconv.Atoi(date[:4])
		return y
	}
	return 0
}

type imgLockEntry struct {
	mu   sync.Mutex
	refs int
}

var (
	reImgFile  = regexp.MustCompile(`^[A-Za-z0-9_-]{5,64}\.(jpg|png)$`)
	imgSizes   = set("w92", "w154", "w185", "w300", "w342", "w500", "w780", "w1280", "original")
	imgFetchMu sync.Mutex
	imgFetches = map[string]*imgLockEntry{} // one lock per in-flight image: cleaned up when done
	imgClient  = &http.Client{Timeout: 30 * time.Second}
)

func acquireImgLock(p string) *imgLockEntry {
	imgFetchMu.Lock()
	e := imgFetches[p]
	if e == nil {
		e = &imgLockEntry{}
		imgFetches[p] = e
	}
	e.refs++
	imgFetchMu.Unlock()
	e.mu.Lock()
	return e
}

func releaseImgLock(p string, e *imgLockEntry) {
	e.mu.Unlock()
	imgFetchMu.Lock()
	e.refs--
	if e.refs == 0 {
		delete(imgFetches, p)
	}
	imgFetchMu.Unlock()
}

// serveImage proxies TMDB artwork and keeps every size it was asked for on
// disk, so the TV's home screen never waits on the internet twice.
func (t *TMDB) serveImage(w http.ResponseWriter, r *http.Request) {
	size, file := r.PathValue("size"), r.PathValue("file")
	if !imgSizes[size] || !reImgFile.MatchString(file) {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(t.dir, "img", size+"_"+file)
	if _, err := os.Stat(p); err != nil {
		e := acquireImgLock(p)
		if _, err := os.Stat(p); err != nil {
			err = fetchTo(r.Context(), "https://image.tmdb.org/t/p/"+size+"/"+file, p)
			if err != nil {
				releaseImgLock(p, e)
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
		}
		releaseImgLock(p, e)
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, p)
}

func fetchTo(ctx context.Context, src, dst string) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", src, nil)
	resp, err := imgClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dst + ".part")
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 20<<20)); err != nil {
		f.Close()
		os.Remove(dst + ".part")
		return err
	}
	f.Close()
	return os.Rename(dst+".part", dst)
}
