package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The player API (/api/v1) is what the TV and phone app talk to. It's served
// on its own port (PlayerListen) that home Wi-Fi may reach, and it can only
// read the library, stream and record progress: queueing and deleting stay
// on the Tailscale-only admin port.

const PlayerListen = ":8790"

type Player struct {
	c *Catalog
	w *Watch
	t *TMDB
	e *Engine
}

var (
	playMu  sync.Mutex
	playing = map[string]time.Time{} // client+item -> last stream/progress activity
)

func touchPlaying(r *http.Request, id string) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	playMu.Lock()
	playing[host+"/"+id] = time.Now()
	playMu.Unlock()
}

// playingNow counts what our own app is playing (for the download throttle).
func playingNow() int {
	playMu.Lock()
	defer playMu.Unlock()
	n := 0
	for k, t := range playing {
		if time.Since(t) < 45*time.Second {
			n++
		} else {
			delete(playing, k)
		}
	}
	return n
}

func (p *Player) mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/hello", p.hello)
	mux.HandleFunc("GET /api/v1/home", p.home)
	mux.HandleFunc("GET /api/v1/movies", func(w http.ResponseWriter, r *http.Request) {
		movies, _ := p.c.Lists()
		writeJSON(w, 200, map[string]any{"items": p.list(movies)})
	})
	mux.HandleFunc("GET /api/v1/shows", func(w http.ResponseWriter, r *http.Request) {
		_, shows := p.c.Lists()
		writeJSON(w, 200, map[string]any{"items": p.list(shows)})
	})
	mux.HandleFunc("GET /api/v1/search", p.search)
	mux.HandleFunc("GET /api/v1/item/{id}", p.item)
	mux.HandleFunc("POST /api/v1/progress", p.progress)
	mux.HandleFunc("POST /api/v1/watched", p.watched)
	mux.HandleFunc("GET /api/v1/stream/{id}/{name...}", p.stream)
	mux.HandleFunc("GET /api/v1/stream/{id}", p.stream)
	mux.HandleFunc("GET /api/v1/sub/{id}/{n}", p.sub)
	mux.HandleFunc("GET /api/v1/img/{size}/{file}", p.t.serveImage)
	mux.HandleFunc("GET /api/v1/match/{id}", p.matchSearch)
	mux.HandleFunc("POST /api/v1/match", p.rematch)
	mux.HandleFunc("GET /api/v1/app", p.appInfo)
	mux.HandleFunc("GET /api/v1/app.apk", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.android.package-archive")
		w.Header().Set("Content-Disposition", `attachment; filename="theater.apk"`)
		http.ServeFile(w, r, filepath.Join(DataDir, "app", "theater.apk"))
	})
}

// list is what the poster rows and grids need: no chapters or cast, which
// only the title page and the player use (/api/v1/item has them). Nearly half
// of Home's size, and the TV parses all of it.
func (p *Player) list(xs []*Item) []*Item {
	out := make([]*Item, len(xs))
	for i, x := range xs {
		it := slimCast(p.w.decorate(x, false))
		if it.Info != nil && len(it.Info.Chapters) > 0 {
			info := *it.Info
			info.Chapters = nil
			it.Info = &info
		}
		out[i] = it
	}
	return out
}

// slimCast drops the cast from an item decorate has already copied.
func slimCast(it *Item) *Item {
	if it.Meta != nil && len(it.Meta.Cast) > 0 {
		m := *it.Meta
		m.Cast = nil
		it.Meta = &m
	}
	return it
}

// hello lets the app find every address this server answers on, so it can
// switch between home Wi-Fi and Tailscale without asking.
func (p *Player) hello(w http.ResponseWriter, r *http.Request) {
	var lan, ts []string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			u := "http://" + ipn.IP.String() + PlayerListen
			if i.Name == "tailscale0" {
				ts = append(ts, u)
			} else if ipn.IP.IsPrivate() {
				lan = append(lan, u)
			}
		}
	}
	host, _ := os.Hostname()
	writeJSON(w, 200, map[string]any{"name": host, "lan": lan, "tailscale": ts,
		"magicdns": "http://mint.tail25d09d.ts.net" + PlayerListen, "app": readAppInfo()})
}

type dlRow struct {
	Title    string `json:"title"`
	State    string `json:"state"`
	Progress int    `json:"progress"`
	Speed    string `json:"speed,omitempty"`
	ETA      string `json:"eta,omitempty"`
}

func (p *Player) home(w http.ResponseWriter, r *http.Request) {
	movies, shows := p.c.Lists()
	cont := p.w.continueRow(movies, shows)
	contOut := make([]*Item, len(cont))
	for i, it := range cont {
		contOut[i] = p.withShow(p.w.decorate(it, false))
	}

	// Recently added: films by file date, shows by their newest episode.
	var recent []*Item
	recent = append(recent, movies...)
	recent = append(recent, shows...)
	sort.SliceStable(recent, func(a, b int) bool { return recent[a].Added > recent[b].Added })
	if len(recent) > 20 {
		recent = recent[:20]
	}

	var dls []dlRow
	if p.e != nil {
		for _, j := range p.e.store.List() {
			if !j.busy() {
				continue
			}
			title := strings.TrimSuffix(filepath.Base(preview(j.Plan).Dest), filepath.Ext(preview(j.Plan).Dest))
			dls = append(dls, dlRow{title, j.State, j.Progress, j.Speed, j.ETA})
		}
	}
	// the phone's featured card can play from Recently Added directly, so it keeps its chapters
	recentOut := make([]*Item, len(recent))
	for i, it := range recent {
		recentOut[i] = slimCast(p.w.decorate(it, false))
	}
	writeJSON(w, 200, map[string]any{
		"continue": contOut, "recent": recentOut, "movies": p.list(movies), "shows": p.list(shows),
		"downloading": dls,
	})
}

// withShow gives an episode its show's artwork and logo, which the
// Continue Watching cards and the player overlay use.
func (p *Player) withShow(it *Item) *Item {
	if it.Type != "episode" {
		return it
	}
	if sh := p.c.Get(it.ShowID); sh != nil && sh.Meta != nil {
		m := TMeta{}
		if it.Meta != nil {
			m = *it.Meta
		}
		if m.Backdrop == "" {
			m.Backdrop = sh.Meta.Backdrop
		}
		if m.Poster == "" {
			m.Poster = sh.Meta.Poster
		}
		m.Logo = sh.Meta.Logo
		if m.Cert == "" {
			m.Cert = sh.Meta.Cert
		}
		it.Meta = &m
		it.Show = sh.Meta.Title
	}
	return it
}

func (p *Player) search(w http.ResponseWriter, r *http.Request) {
	q := normTitle(r.URL.Query().Get("q"))
	movies, shows := p.c.Lists()
	var out []*Item
	if q != "" {
		for _, it := range append(append([]*Item{}, movies...), shows...) {
			t := normTitle(it.Title)
			if it.Meta != nil {
				t += " " + normTitle(it.Meta.Title)
				for _, c := range it.Meta.Cast {
					t += " " + normTitle(c.Name)
				}
			}
			if strings.Contains(t, q) {
				out = append(out, it)
			}
		}
	}
	writeJSON(w, 200, map[string]any{"items": p.list(out)})
}

func (p *Player) item(w http.ResponseWriter, r *http.Request) {
	it := p.c.Get(r.PathValue("id"))
	if it == nil {
		errJSON(w, 404, "not in the library (rescanned?)")
		return
	}
	out := map[string]any{"item": p.withShow(p.w.decorate(it, true))}
	if it.Type == "episode" {
		if n := nextEpisode(p.c, it); n != nil {
			out["next"] = p.withShow(p.w.decorate(n, false))
		}
	}
	if it.Type == "show" || it.Type == "season" {
		// what the big Play button starts: the in-progress or next unseen episode
		eps := episodesOf(it)
		var pick *Item
		for _, e := range eps {
			if s := p.w.Get(e.ID); s == nil || !s.Watched {
				pick = e
				break
			}
		}
		if pick == nil && len(eps) > 0 {
			pick = eps[0]
		}
		if pick != nil {
			out["up_next"] = p.withShow(p.w.decorate(pick, false))
		}
	}
	writeJSON(w, 200, out)
}

func (p *Player) progress(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID    string `json:"id"`
		Pos   int64  `json:"pos"`
		Dur   int64  `json:"dur"`
		Ended bool   `json:"ended"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	it := p.c.Get(b.ID)
	if it == nil || it.file == "" {
		errJSON(w, 404, "unknown item")
		return
	}
	if !b.Ended {
		touchPlaying(r, b.ID)
	}
	writeJSON(w, 200, p.w.Progress(b.ID, b.Pos, b.Dur, b.Ended))
}

func (p *Player) watched(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID      string `json:"id"`
		Watched bool   `json:"watched"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	it := p.c.Get(b.ID)
	if it == nil {
		errJSON(w, 404, "unknown item")
		return
	}
	var ids []string
	for _, e := range episodesOf(it) {
		ids = append(ids, e.ID)
	}
	p.w.SetWatched(ids, b.Watched)
	writeJSON(w, 200, map[string]any{"ok": true, "item": p.w.decorate(it, true)})
}

var videoMime = map[string]string{"mkv": "video/x-matroska", "mp4": "video/mp4", "m4v": "video/mp4", "webm": "video/webm",
	"avi": "video/x-msvideo", "mov": "video/quicktime", "ts": "video/mp2t", "m2ts": "video/mp2t"}

// stream hands out the original file with range support: the TV decodes it
// as-is (direct play), so the server's CPU does nothing and seeking is a
// single HTTP range request.
func (p *Player) stream(w http.ResponseWriter, r *http.Request) {
	it := p.c.Get(r.PathValue("id"))
	if it == nil || it.file == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(it.file)
	if err != nil {
		http.Error(w, "file missing; rescan", 404)
		p.c.Refresh()
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if r.Method == "GET" && r.URL.Query().Get("dl") == "" {
		touchPlaying(r, it.ID)
	}
	h := w.Header()
	if m := videoMime[ext(it.file)]; m != "" {
		h.Set("Content-Type", m)
	}
	if r.URL.Query().Get("dl") != "" { // phone "download for offline"
		h.Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(filepath.Base(it.file), `"`, "")+`"`)
	}
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func (p *Player) sub(w http.ResponseWriter, r *http.Request) {
	it := p.c.Get(r.PathValue("id"))
	n, err := strconv.Atoi(r.PathValue("n"))
	if it == nil || err != nil || n < 0 || n >= len(it.Subs) {
		http.NotFound(w, r)
		return
	}
	s := it.Subs[n]
	w.Header().Set("Content-Type", s.Mime+"; charset=utf-8")
	http.ServeFile(w, r, s.file)
}

func (p *Player) matchSearch(w http.ResponseWriter, r *http.Request) {
	it := p.c.Get(r.PathValue("id"))
	if it == nil || it.Type != "movie" && it.Type != "show" {
		errJSON(w, 404, "only films and shows can be re-matched")
		return
	}
	q := r.URL.Query().Get("q")
	year := 0
	if q == "" {
		q, year = it.Title, it.Year
	}
	kind := map[string]string{"movie": "movie", "show": "tv"}[it.Type]
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := p.t.Search(ctx, kind, q, year)
	if err != nil {
		errJSON(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"results": res})
}

func (p *Player) rematch(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID   string `json:"id"`
		TMDB int    `json:"tmdb"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	it := p.c.Get(b.ID)
	if it == nil || b.TMDB <= 0 {
		errJSON(w, 400, "need an item id and a TMDB id")
		return
	}
	if err := p.t.Rematch(it, b.TMDB); err != nil {
		errJSON(w, 502, err.Error())
		return
	}
	p.c.Refresh()
	writeJSON(w, 200, map[string]any{"ok": true})
}

// AppInfo is written next to the APK by android/build.sh.
type AppInfo struct {
	VersionCode int    `json:"version_code"`
	VersionName string `json:"version_name"`
	Size        int64  `json:"size"`
}

func readAppInfo() *AppInfo {
	var a AppInfo
	b, err := os.ReadFile(filepath.Join(DataDir, "app", "theater.json"))
	if err != nil || json.Unmarshal(b, &a) != nil {
		return nil
	}
	return &a
}

func (p *Player) appInfo(w http.ResponseWriter, r *http.Request) {
	if a := readAppInfo(); a != nil {
		writeJSON(w, 200, a)
		return
	}
	errJSON(w, 404, "no app build yet")
}

// servePlayer runs the player API on its own port. Firewall: home Wi-Fi +
// tailscale0 only.
func servePlayer(p *Player) *http.Server {
	mux := http.NewServeMux()
	p.mount(mux)
	mountDAV(mux)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("Theater player API. Get the TV/phone app: /api/v1/app.apk\n"))
	})
	addr := PlayerListen
	if v := os.Getenv("THEATER_PLAYER_LISTEN"); v != "" {
		addr = v
	}
	hs := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("player API listening on %s", addr)
		if err := hs.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("player API: %v", err)
		}
	}()
	return hs
}
