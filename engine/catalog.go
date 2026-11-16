package main

import (
	"crypto/sha1"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Item is a film, show, season or episode as the TV/phone app sees it.
// IDs are derived from the path, so they survive restarts and rescans
// (and a Dolby Vision fix that rewrites a file under the same name).
type Item struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"` // movie | show | season | episode
	Title   string  `json:"title"`
	Year    int     `json:"year,omitempty"`
	ShowID  string  `json:"show_id,omitempty"`
	Show    string  `json:"show,omitempty"`
	Season  int     `json:"season,omitempty"`
	Episode int     `json:"episode,omitempty"`
	Added   int64   `json:"added"` // unix seconds (newest file for shows/seasons)
	Size    int64   `json:"size,omitempty"`
	Info    *MInfo  `json:"info,omitempty"`
	Subs    []XSub  `json:"subs,omitempty"`    // subtitle files next to the video
	Intro   *Span   `json:"intro,omitempty"`   // skip-intro marker (episodes)
	Credits *Span   `json:"credits,omitempty"` // end credits start (from chapters)
	Thumbs  int     `json:"thumbs,omitempty"`  // scrub pictures available (/api/v1/thumbs/{id})
	Light   *Light  `json:"light,omitempty"`   // a smaller copy for browsers and slow connections
	Count   int     `json:"count,omitempty"`   // episodes in a show / season
	Meta    *TMeta  `json:"meta,omitempty"`
	Watch   *WState `json:"watch,omitempty"`  // filled per response
	Unseen  int     `json:"unseen,omitempty"` // shows/seasons: episodes not watched yet
	Seasons []*Item `json:"seasons,omitempty"`
	Eps     []*Item `json:"episodes,omitempty"`

	file   string // absolute path of the video (movies, episodes)
	folder string // movie / show folder, relative to Media (for TMDB matching)
}

// Light is a title's smaller copy (mp4.go), streamed with ?v=light.
type Light struct {
	Size int64  `json:"size"`
	Info *MInfo `json:"info,omitempty"`
	file string
}

// XSub is a sidecar subtitle file (Movie.en.srt and friends).
type XSub struct {
	Lang   string `json:"lang,omitempty"`
	Label  string `json:"label"`
	Mime   string `json:"mime"`
	Forced bool   `json:"forced,omitempty"`
	file   string
}

type Catalog struct {
	mu      sync.RWMutex
	items   map[string]*Item
	movies  []*Item
	shows   []*Item
	at      time.Time
	refresh chan struct{}
	tmdb    *TMDB
	intros  *Intros
	thumbs  *Thumbs
	ready   chan struct{} // closed after the first scan
	noBg    bool          // skips background passes (tests)
}

func NewCatalog(t *TMDB, in *Intros, th *Thumbs) *Catalog {
	c := &Catalog{intros: in, thumbs: th, items: map[string]*Item{}, refresh: make(chan struct{}, 1), tmdb: t, ready: make(chan struct{})}
	go c.loop()
	return c
}

// Refresh asks for a rescan soon (coalesced; downloads, deletes and the
// 15-minute tick all land here).
func (c *Catalog) Refresh() {
	select {
	case c.refresh <- struct{}{}:
	default:
	}
}

func (c *Catalog) loop() {
	sig := folderSig()
	c.scan()
	close(c.ready)
	full := time.NewTicker(15 * time.Minute)
	peek := time.NewTicker(time.Minute)
	for {
		select {
		case <-c.refresh:
			time.Sleep(2 * time.Second)
		case <-full.C:
		case <-peek.C: // files copied in by hand (Taildrop, scp) show up within a minute
			if s := folderSig(); s != sig {
				sig = s
				break
			}
			continue
		}
		sig = folderSig()
		c.scan()
	}
}

// folderSig sums the modification times of the library's folders two levels
// deep (a film folder, a show's season folders): a handful of stats that
// change whenever something is added, renamed or removed.
func folderSig() int64 {
	var sig int64
	for _, sec := range []string{"Movies", "TV Shows"} {
		root := filepath.Join(Media, sec)
		dirs := []string{root}
		l1, _ := os.ReadDir(root)
		for _, e := range l1 {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				p := filepath.Join(root, e.Name())
				dirs = append(dirs, p)
				l2, _ := os.ReadDir(p)
				for _, f := range l2 {
					if f.IsDir() && !strings.HasPrefix(f.Name(), ".") {
						dirs = append(dirs, filepath.Join(p, f.Name()))
					}
				}
			}
		}
		for _, d := range dirs {
			if fi, err := os.Stat(d); err == nil {
				sig = sig*31 + fi.ModTime().UnixNano()
			}
		}
	}
	return sig
}

func pathID(rel string) string {
	sum := sha1.Sum([]byte(rel))
	return hex.EncodeToString(sum[:6])
}

var (
	reTitleYear = regexp.MustCompile(`^(.*?)\s*\((\d{4})\)\s*$`)
	reSeasonDir = regexp.MustCompile(`(?i)^(?:season|series|s)\s*(\d{1,3})$`)
)

func splitTitleYear(name string) (string, int) {
	if m := reTitleYear.FindStringSubmatch(name); m != nil {
		y, _ := strconv.Atoi(m[2])
		return m[1], y
	}
	return name, 0
}

func (c *Catalog) scan() {
	start := time.Now()
	items := map[string]*Item{}
	var movies, shows []*Item

	movieRoot := scanTree(filepath.Join(Media, "Movies"))
	for _, d := range movieRoot.Children {
		var video *LNode
		var walk func(n *LNode)
		walk = func(n *LNode) { // biggest video in the folder is the film
			for _, ch := range n.Children {
				if ch.Dir {
					walk(ch)
				} else if VideoExt[ext(ch.Name)] && !isSample(filepath.Join(Media, ch.Path)) && (video == nil || ch.Size > video.Size) {
					video = ch
				}
			}
		}
		if d.Dir {
			walk(d)
		} else if VideoExt[ext(d.Name)] {
			video = d
		}
		if video == nil {
			continue
		}
		name := d.Name
		if !d.Dir {
			name = strings.TrimSuffix(name, filepath.Ext(name))
		}
		title, year := splitTitleYear(name)
		m := &Item{ID: pathID(video.Path), Type: "movie", Title: title, Year: year, Added: int64(video.Added),
			Size: video.Size, file: filepath.Join(Media, video.Path), folder: d.Path}
		movies = append(movies, m)
		items[m.ID] = m
	}

	showRoot := scanTree(filepath.Join(Media, "TV Shows"))
	for _, d := range showRoot.Children {
		if !d.Dir {
			continue
		}
		title, year := splitTitleYear(d.Name)
		show := &Item{ID: pathID(d.Path), Type: "show", Title: title, Year: year, folder: d.Path}
		seasons := map[int]*Item{}
		var walk func(n *LNode, seasonHint int)
		walk = func(n *LNode, hint int) {
			for _, ch := range n.Children {
				if ch.Dir {
					h := hint
					if m := reSeasonDir.FindStringSubmatch(ch.Name); m != nil {
						h, _ = strconv.Atoi(m[1])
					} else if strings.EqualFold(ch.Name, "Specials") {
						h = 0
					}
					walk(ch, h)
					continue
				}
				if !VideoExt[ext(ch.Name)] || isSample(filepath.Join(Media, ch.Path)) {
					continue
				}
				s, e, named := epParts(strings.TrimSuffix(ch.Name, filepath.Ext(ch.Name)))
				if !named && hint > 0 {
					s = hint
				}
				if e == 0 {
					continue
				}
				sn := seasons[s]
				if sn == nil {
					sn = &Item{ID: pathID(d.Path + "#s" + strconv.Itoa(s)), Type: "season", Title: seasonName(s),
						ShowID: show.ID, Show: title, Season: s}
					seasons[s] = sn
				}
				ep := &Item{ID: pathID(ch.Path), Type: "episode", Title: "Episode " + strconv.Itoa(e),
					ShowID: show.ID, Show: title, Season: s, Episode: e, Added: int64(ch.Added), Size: ch.Size,
					file: filepath.Join(Media, ch.Path)}
				sn.Eps = append(sn.Eps, ep)
				items[ep.ID] = ep
			}
		}
		walk(d, 0)
		if len(seasons) == 0 {
			continue
		}
		for _, sn := range seasons {
			sort.Slice(sn.Eps, func(a, b int) bool { return sn.Eps[a].Episode < sn.Eps[b].Episode })
			for _, ep := range sn.Eps {
				sn.Added = max(sn.Added, ep.Added)
				sn.Size += ep.Size
			}
			sn.Count = len(sn.Eps)
			show.Seasons = append(show.Seasons, sn)
			show.Added = max(show.Added, sn.Added)
			show.Size += sn.Size
			show.Count += sn.Count
			items[sn.ID] = sn
		}
		sort.Slice(show.Seasons, func(a, b int) bool { // specials last
			sa, sb := show.Seasons[a].Season, show.Seasons[b].Season
			if (sa == 0) != (sb == 0) {
				return sb == 0
			}
			return sa < sb
		})
		shows = append(shows, show)
		items[show.ID] = show
	}

	// picture/sound details and sidecar subtitles (ffprobe results are cached)
	seen := map[string]bool{}
	for _, it := range items {
		if it.file == "" {
			continue
		}
		seen[it.file] = true
		if fi, err := os.Stat(it.file); err == nil {
			it.Info = mediaInfo(it.file, fi)
		}
		it.Subs = sidecarSubs(it.file)
		if lf := findLight(it.file); lf != "" {
			if fi, err := os.Stat(lf); err == nil {
				seen[lf] = true
				it.Light = &Light{Size: fi.Size(), Info: mediaInfo(lf, fi), file: lf}
			}
		}
		it.Thumbs = c.thumbs.Count(it.file)
		if it.Info != nil {
			it.Intro = chapterIntro(it.Info.Chapters)
			it.Credits = chapterCredits(it.Info.Chapters)
		}
		if it.Intro == nil && it.Type == "episode" {
			it.Intro = c.intros.Get(it.file)
		}
	}
	saveMediaInfo(seen)

	byTitle := func(xs []*Item) {
		sort.Slice(xs, func(a, b int) bool { return sortKey(xs[a].Title) < sortKey(xs[b].Title) })
	}
	byTitle(movies)
	byTitle(shows)

	c.tmdb.Attach(movies, shows) // cached metadata now; missing matches are fetched in the background

	c.mu.Lock()
	c.items, c.movies, c.shows, c.at = items, movies, shows, time.Now()
	c.mu.Unlock()
	log.Printf("catalog: %d movies, %d shows, %d items in %s", len(movies), len(shows), len(items),
		time.Since(start).Round(time.Millisecond))
	if c.noBg {
		return
	}

	if c.intros.Pending(shows) {
		go func() {
			if c.intros.Analyse(shows) {
				c.Refresh()
			}
		}()
	}
	go fillSubs(items) // subtitles inside the files, read out for browsers
	if c.thumbs.Pending(items) {
		go c.thumbs.Generate(items, c.Refresh)
	}
	if c.tmdb.HasMissing(movies, shows) {
		go func() {
			if c.tmdb.FetchMissing(movies, shows) {
				c.Refresh()
			}
		}()
	}
}

func seasonName(n int) string {
	if n == 0 {
		return "Specials"
	}
	return "Season " + strconv.Itoa(n)
}

// sortKey drops a leading article, like Plex does ("The Wire" sorts under W).
func sortKey(t string) string {
	t = strings.ToLower(t)
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(t, a) && len(t) > len(a) {
			return t[len(a):]
		}
	}
	return t
}

var subMimes = map[string]string{"srt": "application/x-subrip", "ass": "text/x-ssa", "ssa": "text/x-ssa", "vtt": "text/vtt"}

// sidecarSubs finds "<video base>[.lang][.forced].srt" files next to a video.
func sidecarSubs(video string) []XSub {
	dir := filepath.Dir(video)
	base := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	ents, _ := os.ReadDir(dir)
	var out []XSub
	for _, e := range ents {
		n := e.Name()
		mime := subMimes[ext(n)]
		if e.IsDir() || mime == "" || !strings.HasPrefix(n, base) {
			continue
		}
		s := XSub{Mime: mime, file: filepath.Join(dir, n)}
		for _, tag := range strings.Split(strings.Trim(strings.TrimSuffix(n[len(base):], filepath.Ext(n)), "."), ".") {
			switch t := strings.ToLower(tag); {
			case t == "forced":
				s.Forced = true
			case t == "sdh" || t == "cc" || t == "hi":
				s.Label = "SDH"
			case langName(t) != "":
				s.Lang = t
			}
		}
		label := langName(s.Lang)
		if label == "" {
			label = "Subtitles"
		}
		if s.Label != "" {
			label += " " + s.Label
		}
		if s.Forced {
			label += " (forced)"
		}
		s.Label = label
		out = append(out, s)
	}
	return out
}

func (c *Catalog) Get(id string) *Item {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.items[id]
}

// All returns every item (films, shows, seasons, episodes).
func (c *Catalog) All() []*Item {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*Item, 0, len(c.items))
	for _, it := range c.items {
		out = append(out, it)
	}
	return out
}

func (c *Catalog) Lists() (movies, shows []*Item) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.movies, c.shows
}

// episodesOf lists a show's or season's episodes in viewing order.
func episodesOf(it *Item) []*Item {
	switch it.Type {
	case "episode", "movie":
		return []*Item{it}
	case "season":
		return it.Eps
	}
	var out []*Item
	for _, s := range it.Seasons {
		if s.Season > 0 {
			out = append(out, s.Eps...)
		}
	}
	for _, s := range it.Seasons { // specials after the main run
		if s.Season == 0 {
			out = append(out, s.Eps...)
		}
	}
	return out
}
