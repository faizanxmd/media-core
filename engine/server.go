package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

type asset struct {
	ctype, etag string
	raw, gz     []byte
}

// loadAssets reads the embedded UI once, with gzip copies and ETags.
func loadAssets() map[string]*asset {
	out := map[string]*asset{}
	sub, _ := fs.Sub(webFS, "web")
	fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := fs.ReadFile(sub, p)
		sum := sha256.Sum256(b)
		a := &asset{ctype: mime.TypeByExtension(path.Ext(p)), etag: `"` + hex.EncodeToString(sum[:8]) + `"`, raw: b}
		if strings.HasSuffix(p, ".webmanifest") {
			a.ctype = "application/manifest+json"
		}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		zw.Write(b)
		zw.Close()
		if buf.Len() < len(b) {
			a.gz = buf.Bytes()
		}
		out["/"+p] = a
		return nil
	})
	for _, n := range []int{180, 192, 512} {
		b := iconPNG(n)
		sum := sha256.Sum256(b)
		out[fmt.Sprintf("/icon-%d.png", n)] = &asset{ctype: "image/png", etag: `"` + hex.EncodeToString(sum[:8]) + `"`, raw: b}
	}
	out["/apple-touch-icon.png"] = out["/icon-180.png"]
	out["/"] = out["/index.html"]
	return out
}

func serveAsset(w http.ResponseWriter, r *http.Request, a *asset) {
	h := w.Header()
	h.Set("Content-Type", a.ctype)
	h.Set("ETag", a.etag)
	h.Set("Cache-Control", "no-cache") // revalidate: cheap 304s, never stale after an update
	h.Set("Vary", "Accept-Encoding")
	if r.Header.Get("If-None-Match") == a.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if a.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		w.Write(a.gz)
		return
	}
	w.Write(a.raw)
}

type Server struct {
	e      *Engine
	p      *Player
	assets map[string]*asset
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"ok": false, "error": msg})
}

// readJSON insists on application/json: browsers can't send that cross-site
// without a CORS preflight (which we never answer), so random web pages
// can't queue downloads through a visitor's browser.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		errJSON(w, http.StatusUnsupportedMediaType, "send JSON (Content-Type: application/json)")
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		errJSON(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return false
	}
	return true
}

type Status struct {
	Jobs      []Job    `json:"jobs"`
	FreeGB    int      `json:"free_gb"`
	DiskFree  int64    `json:"disk_free"`
	DiskTotal int64    `json:"disk_total"`
	SpeedBps  int64    `json:"speed_bps"`
	Aria2OK   bool     `json:"aria2_ok"`
	Streams   int      `json:"streams"`
	Settings  Settings `json:"settings"`
}

func (s *Server) status() Status {
	jobs := s.e.store.List()
	for i := range jobs {
		jobs[i].Dest = preview(jobs[i].Plan).Dest
	}
	free, total := disk()
	s.e.mu.Lock()
	st := Status{Jobs: jobs, FreeGB: int(free / 1e9), DiskFree: free, DiskTotal: total,
		SpeedBps: s.e.speed, Aria2OK: s.e.ariaOK, Streams: playingNow(),
		Settings: s.e.settings.get()}
	s.e.mu.Unlock()
	return st
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if a := s.assets[r.URL.Path]; a != nil {
			serveAsset(w, r, a)
			return
		}
		http.NotFound(w, r)
	})
	// what the web player saw in the browser (player.js, wlog): errors, refused plays, what it can decode.
	// Lands in the engine's journal, so a problem in Safari can be read here on mint.
	mux.HandleFunc("POST /api/weblog", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			UA     string   `json:"ua"`
			Events []string `json:"events"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&b) != nil {
			w.WriteHeader(400)
			return
		}
		who := browserName(b.UA)
		for i, e := range b.Events {
			if i == 40 {
				break
			}
			if len(e) > 400 {
				e = e[:400] + "…"
			}
			log.Printf("web %s: %s", who, strings.ReplaceAll(e, "\n", " "))
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.status()) })
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.status()) })
	mux.HandleFunc("GET /api/events", s.events)

	mux.HandleFunc("POST /api/probe", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ URL string }
		if readJSON(w, r, &b) {
			res := s.e.Probe(r.Context(), b.URL)
			if res.Media != nil { // where it will play, for the browser that asked
				res.Play = howToPlay(res.Media, nil, capsOf(r)).How
				res.Browser = res.Play != "none"
			}
			writeJSON(w, 200, res)
		}
	})
	mux.HandleFunc("POST /api/plan", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Plan Plan }
		if readJSON(w, r, &b) {
			writeJSON(w, 200, preview(b.Plan))
		}
	})
	mux.HandleFunc("POST /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Probe Probe
			Plan  Plan
			Light *Probe // a Light copy to download right after it
		}
		if !readJSON(w, r, &b) {
			return
		}
		var lp Plan
		if b.Light != nil {
			_, fn, final, err := planPaths(b.Plan)
			if err != nil || b.Light.Plan == nil || b.Plan.Kind == "tv" && fn == "" || b.Plan.Kind == "music" {
				errJSON(w, 400, "a Light copy goes with one film or one episode")
				return
			}
			if err := lightFits(b.Probe.Media, b.Light.Media); err != nil {
				errJSON(w, 400, err.Error())
				return
			}
			lp = lightPlan(b.Plan.Kind, b.Plan.Title, rel(final), *b.Light)
			lp.Year, lp.Season, lp.Episode = b.Plan.Year, b.Plan.Season, b.Plan.Episode
		}
		j, err := s.e.NewJob(b.Probe, b.Plan)
		if err != nil {
			errJSON(w, 400, err.Error())
			return
		}
		if b.Light != nil {
			if _, err := s.e.NewJob(*b.Light, lp); err != nil {
				errJSON(w, 400, "the film is queued, but its Light copy isn't: "+err.Error())
				return
			}
		}
		j.Dest = preview(j.Plan).Dest
		writeJSON(w, 200, j)
	})
	// One call from a phone share sheet / Shortcut / CLI: check the link,
	// take the guessed name (or the overrides given) and queue it.
	mux.HandleFunc("POST /api/quick", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			URL     string
			Kind    string
			Title   string
			Year    Flex
			Season  Flex
			Episode Flex
		}
		if !readJSON(w, r, &b) {
			return
		}
		if m := reURL.FindString(b.URL); m != "" { // shared text often wraps the link
			b.URL = m
		}
		p := s.e.Probe(r.Context(), b.URL)
		if !p.OK {
			writeJSON(w, 200, map[string]any{"ok": false, "error": p.Error, "message": "✗ " + p.Error})
			return
		}
		plan := *p.Plan
		if b.Kind != "" {
			plan.Kind = b.Kind
		}
		if b.Title != "" {
			plan.Title = b.Title
		}
		for _, f := range []struct {
			dst *Flex
			v   Flex
		}{{&plan.Year, b.Year}, {&plan.Season, b.Season}, {&plan.Episode, b.Episode}} {
			if f.v != "" {
				*f.dst = f.v
			}
		}
		j, err := s.e.NewJob(p, plan)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error(), "message": "✗ " + err.Error()})
			return
		}
		msg := "Queued " + preview(plan).Dest
		if p.Size != nil {
			msg += " · " + human(*p.Size)
		}
		if p.EtaMin != nil && *p.EtaMin > 0 {
			msg += fmt.Sprintf(" · ~%s", dur(int64(*p.EtaMin)*60))
		}
		for _, wn := range p.Warnings {
			msg += "\n⚠ " + wn
		}
		writeJSON(w, 200, map[string]any{"ok": true, "job": j, "message": msg})
	})
	mux.HandleFunc("POST /api/jobs/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			URL    string
			Delete bool
		}
		if !readJSON(w, r, &b) {
			return
		}
		action := r.PathValue("action")
		j, err := s.e.Action(r.PathValue("id"), action, b.URL, b.Delete)
		if err != nil {
			errJSON(w, 400, err.Error())
			return
		}
		if action == "remove" {
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
		writeJSON(w, 200, j)
	})
	mux.HandleFunc("POST /api/clear", func(w http.ResponseWriter, r *http.Request) {
		var b struct{}
		if !readJSON(w, r, &b) {
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "removed": s.e.ClearFinished()})
	})
	mux.HandleFunc("GET /api/library", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, Library(s.p))
	})
	mux.HandleFunc("GET /api/cover", func(w http.ResponseWriter, r *http.Request) {
		abs, _, err := libraryTarget(r.URL.Query().Get("p"))
		if err != nil || !reCover.MatchString(filepath.Base(abs)) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, abs)
	})
	mux.HandleFunc("POST /api/library/delete", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Path string }
		if !readJSON(w, r, &b) {
			return
		}
		freed, err := s.e.DeleteFromLibrary(b.Path)
		if err != nil {
			errJSON(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "freed": freed})
	})
	s.p.mount(mux) // the app's API is reachable here too (Tailscale)
	mountWebCopies(mux, s.p)
	s.mountManager(mux)
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.e.settings.get())
	})
	mux.HandleFunc("POST /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]json.RawMessage
		if !readJSON(w, r, &b) {
			return
		}
		writeJSON(w, 200, s.e.SetSettings(func(st *Settings) {
			// only the fields sent are changed
			cur, _ := json.Marshal(st)
			var m map[string]json.RawMessage
			json.Unmarshal(cur, &m)
			for k, v := range b {
				if k != "avg_bps" {
					m[k] = v
				}
			}
			merged, _ := json.Marshal(m)
			json.Unmarshal(merged, st)
		}))
	})
	return logRequests(mux)
}

// events streams the full status whenever something changes (SSE), at most
// ~3 times a second, so the page never polls.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	ch := s.e.store.Subscribe()
	defer s.e.store.Unsubscribe(ch)
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	var lastSent []byte
	send := func() {
		b, _ := json.Marshal(s.status())
		if bytes.Equal(b, lastSent) {
			return
		}
		lastSent = b
		fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}
	fmt.Fprint(w, "retry: 2000\n\n")
	send()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			send()
			time.Sleep(300 * time.Millisecond) // coalesce bursts
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		h.ServeHTTP(w, r)
	})
}

func serve() {
	log.SetFlags(0)
	store := NewStore()
	aria, err := NewAria2()
	if err != nil {
		log.Fatal(err)
	}
	e := NewEngine(store, aria, loadSettings())
	tm := NewTMDB()
	cat := NewCatalog(tm, NewIntros(), NewThumbs())
	watch := NewWatch()
	e.onLibrary = cat.Refresh
	e.onMoved = func(oldPath, newPath string) {
		watch.Move(pathID(rel(oldPath)), pathID(rel(newPath)))
		watch.Flush()
	}
	go e.Run()
	pl := &Player{c: cat, w: watch, t: tm, e: e}
	gate := newOwnerGate()
	ps := servePlayer(pl, gate)
	srv := &Server{e: e, p: pl, assets: loadAssets()}
	addr := Listen
	if v := os.Getenv("MEDIA_CORE_LISTEN"); v != "" {
		addr = v
	}
	hs := &http.Server{Addr: addr, Handler: gate.wrap(srv.routes()), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-shutdownSignal()
		log.Print("stopping")
		store.Flush()
		watch.Flush()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ps.Shutdown(ctx)
		hs.Shutdown(ctx)
	}()
	log.Printf("media-core-engine listening on %s", addr)
	if err := hs.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
	store.Flush()
}

// browserName: "Safari 18.1" or "Chrome 131" from a User-Agent, for the log.
func browserName(ua string) string {
	field := func(key string) string {
		i := strings.Index(ua, key)
		if i < 0 {
			return ""
		}
		v := ua[i+len(key):]
		if j := strings.IndexAny(v, " ;)"); j >= 0 {
			v = v[:j]
		}
		return v
	}
	switch {
	case strings.Contains(ua, "Edg/"):
		return "Edge " + field("Edg/")
	case strings.Contains(ua, "Chrome/"):
		return "Chrome " + field("Chrome/")
	case strings.Contains(ua, "Firefox/"):
		return "Firefox " + field("Firefox/")
	case strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/"):
		return "Safari " + field("Version/")
	}
	return "browser"
}
