package main

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Manager (the site's Manager tab): the library's files with where each one
// plays, the MP4 step and Light copies for titles already in the library, and
// how mint is doing. All of it is on the Tailscale-only admin port.

type mFile struct {
	ID      string              `json:"id"`
	Path    string              `json:"path"` // library path, for Delete
	Size    int64               `json:"size"`
	Info    *MInfo              `json:"info,omitempty"`
	Light   *Light              `json:"light,omitempty"`
	Browser bool                `json:"browser"` // plays in the browser that asked, one way or another
	Play    string              `json:"play"`    // how: direct | repack | light | none (play.go)
	On      map[string]OnDevice `json:"on"`      // this browser ("here"), the TV and the phone: plays or not, and why
	CanMP4  bool                `json:"can_mp4"`
	Heavy   bool                `json:"heavy"`
	Busy    bool                `json:"busy,omitempty"` // a job is working on it
}

type mTitle struct {
	mFile
	Type    string     `json:"type"` // movie | show
	Title   string     `json:"title"`
	Year    int        `json:"year,omitempty"`
	Poster  string     `json:"poster,omitempty"`
	Added   int64      `json:"added"`
	Seasons int        `json:"seasons,omitempty"`
	Eps     []mEpisode `json:"episodes,omitempty"`
}

type mEpisode struct {
	mFile
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
	Title   string `json:"title"`
}

func (s *Server) busyFiles() map[string]bool {
	out := map[string]bool{}
	for _, j := range s.e.store.List() {
		if !j.busy() {
			continue
		}
		if _, _, final, err := planPaths(j.Plan); err == nil {
			out[final] = true
			if j.Plan.Into != "" {
				out[filepath.Join(Media, j.Plan.Into)] = true
			}
		}
		for _, f := range j.Files {
			out[f] = true
		}
	}
	return out
}

// slim drops what Manager doesn't show (chapters are most of a file's details).
func slim(m *MInfo) *MInfo {
	if m == nil {
		return nil
	}
	cp := *m
	cp.Chapters = nil
	return &cp
}

func fileOf(it *Item, busy map[string]bool, c Caps) mFile {
	f := mFile{ID: it.ID, Path: rel(it.file), Size: it.Size, Info: slim(it.Info), Light: it.Light,
		CanMP4: canMP4(it.Info), Heavy: heavy(it.Info)}
	p, _, _ := webPlay(it, c)
	f.Play = p.How
	if p.Light && p.How != "none" {
		f.Play = "light"
	}
	if f.Play == "none" && webCopy(it) != nil {
		f.Play = "direct" // an old copy made ahead
	}
	f.Browser = f.Play != "none"
	var light *MInfo
	if it.Light != nil {
		light = it.Light.Info
	}
	f.On = onDevices(it.Info, light, c)
	if f.Browser && !f.On["here"].OK { // an old copy made ahead
		f.On["here"] = OnDevice{OK: true, How: "direct"}
	}
	if it.Light != nil {
		f.Size += it.Light.Size
	}
	f.Busy = busy[it.file] || busy[filepath.Dir(it.file)]
	return f
}

func (s *Server) titles(c Caps) []mTitle {
	movies, shows := s.p.c.Lists()
	busy := s.busyFiles()
	var out []mTitle
	name := func(it *Item) (string, int, string) {
		t, y, p := it.Title, it.Year, ""
		if it.Meta != nil {
			if it.Meta.Title != "" {
				t = it.Meta.Title
			}
			if y == 0 {
				y = it.Meta.Year
			}
			p = it.Meta.Poster
		}
		return t, y, p
	}
	for _, m := range movies {
		t, y, p := name(m)
		mt := mTitle{mFile: fileOf(m, busy, c), Type: "movie", Title: t, Year: y, Poster: p, Added: m.Added}
		mt.Path = filepath.Dir(rel(m.file))
		out = append(out, mt)
	}
	for _, sh := range shows {
		t, y, p := name(sh)
		mt := mTitle{Type: "show", Title: t, Year: y, Poster: p, Added: sh.Added, Seasons: len(sh.Seasons)}
		mt.ID, mt.Path = sh.ID, sh.folder
		for _, ep := range episodesOf(sh) {
			f := fileOf(ep, busy, c)
			et := ep.Title
			if ep.Meta != nil && ep.Meta.Title != "" {
				et = ep.Meta.Title
			}
			mt.Eps = append(mt.Eps, mEpisode{mFile: f, Season: ep.Season, Episode: ep.Episode, Title: et})
			mt.Size += f.Size
			mt.Busy = mt.Busy || f.Busy
		}
		out = append(out, mt)
	}
	return out
}

// convertJob makes an MP4 of a file already in the library, through the same
// queue as downloads (one heavy job at a time).
func (s *Server) convertJob(it *Item) (Job, error) {
	if it == nil || it.file == "" {
		return Job{}, errors.New("not a film or an episode")
	}
	if !canMP4(it.Info) {
		return Job{}, errors.New("an MP4 wouldn't make this play in browsers")
	}
	if s.busyFiles()[it.file] {
		return Job{}, errors.New("a job is already working on this")
	}
	title := it.Title
	if it.Type == "episode" {
		title = it.Show + " · " + "S" + pad2(it.Season) + "E" + pad2(it.Episode)
	} else if it.Meta != nil && it.Meta.Title != "" {
		title = it.Meta.Title
	}
	j := &Job{ID: newID(), Created: now(), State: Processing, Filename: filepath.Base(it.file),
		Plan:       Plan{Kind: "convert", Path: rel(it.file), Title: title, Ext: ext(it.file)},
		Downloaded: true, Placed: true, Files: []string{it.file}, TotalBytes: it.Size, Log: []string{}}
	logLine(j, "Make MP4 of "+rel(it.file))
	s.e.store.Add(j)
	s.e.startPost(j.ID)
	return *j, nil
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// lightPlan is the plan for a Light copy of the library file or film folder at into.
func lightPlan(kind, title string, into string, lp Probe) Plan {
	return Plan{Kind: kind, Title: title, Light: true, Into: into, Ext: lp.Plan.Ext, MP4: true}
}

func lightPlanFor(it *Item, lp Probe) Plan {
	if it.Type == "episode" {
		title := it.Show
		if title == "" {
			title = it.Title
		}
		p := lightPlan("tv", title, rel(it.file), lp)
		p.Season = Flex(itoa(it.Season))
		p.Episode = Flex(itoa(it.Episode))
		return p
	}
	title, year := it.Title, it.Year
	if it.Meta != nil {
		if it.Meta.Title != "" {
			title = it.Meta.Title
		}
		if year == 0 {
			year = it.Meta.Year
		}
	}
	p := lightPlan("movie", title, rel(it.file), lp)
	if year > 0 {
		p.Year = Flex(itoa(year))
	}
	return p
}

// ---------- mint ----------

func meminfo() (avail, total int64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), ":")
		n, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
		switch k {
		case "MemAvailable":
			avail = n * 1024
		case "MemTotal":
			total = n * 1024
		}
	}
	return
}

func cpuTemp() float64 {
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	for _, z := range zones {
		t, _ := os.ReadFile(filepath.Join(z, "type"))
		if strings.TrimSpace(string(t)) != "x86_pkg_temp" {
			continue
		}
		v, _ := os.ReadFile(filepath.Join(z, "temp"))
		n, _ := strconv.Atoi(strings.TrimSpace(string(v)))
		return float64(n) / 1000
	}
	return 0
}

func dirSize(root string) int64 {
	var n int64
	filepath.Walk(root, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi.Mode().IsRegular() {
			n += fi.Size()
		}
		return nil
	})
	return n
}

func lastBackup() int64 {
	var best int64
	ents, _ := os.ReadDir(filepath.Join(DataDir, "backups", "state"))
	for _, e := range ents {
		if fi, err := e.Info(); err == nil && strings.HasPrefix(e.Name(), "state-") {
			best = max(best, fi.ModTime().Unix())
		}
	}
	return best
}

func (s *Server) system() map[string]any {
	free, total := disk()
	dfree, dtotal := diskAt(DataDir)
	var light int64
	for _, it := range s.p.c.All() {
		if it.Light != nil {
			light += it.Light.Size
		}
	}
	avail, mem := meminfo()
	up := 0.0
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		up, _ = strconv.ParseFloat(strings.Fields(string(b))[0], 64)
	}
	s.e.mu.Lock()
	ariaOK := s.e.ariaOK
	s.e.mu.Unlock()
	load, cores := loadAvg()
	now := s.p.playingList()
	if now == nil {
		now = []Playing{}
	}
	devs := devices()
	if devs == nil {
		devs = []Device{}
	}
	return map[string]any{
		"load": load, "cores": cores,
		"now":     now,  // what's playing, and on what
		"devices": devs, // the owner's other machines, and whether each reaches mint directly
		"library": map[string]any{"free": free, "total": total,
			"films": scanTree(filepath.Join(Media, "Movies")).Size, "shows": scanTree(filepath.Join(Media, "TV Shows")).Size,
			"music": scanTree(filepath.Join(Media, "Music")).Size, "light": light},
		"data":      map[string]any{"free": dfree, "total": dtotal, "web": dirSize(filepath.Join(DataDir, "web"))},
		"aria2":     ariaOK,
		"playing":   playingNow(),
		"temp_c":    cpuTemp(),
		"mem_avail": avail, "mem_total": mem,
		"backup_at": lastBackup(),
		"uptime_s":  int64(up),
	}
}

func (s *Server) mountManager(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/manager/titles", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"titles": s.titles(capsOf(r))})
	})
	mux.HandleFunc("GET /api/system", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.system()) })
	mux.HandleFunc("POST /api/manager/mp4", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ ID string }
		if !readJSON(w, r, &b) {
			return
		}
		j, err := s.convertJob(s.p.c.Get(b.ID))
		if err != nil {
			errJSON(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, j)
	})
	// a Light copy for a title already in the library: the link was checked first (/api/probe)
	mux.HandleFunc("POST /api/manager/light", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ID    string
			Probe Probe
		}
		if !readJSON(w, r, &b) {
			return
		}
		it := s.p.c.Get(b.ID)
		if it == nil || it.file == "" || b.Probe.Plan == nil {
			errJSON(w, 400, "not a film or an episode")
			return
		}
		busy := s.busyFiles()
		if busy[it.file] || (filepath.Dir(it.file) != filepath.Join(Media, "Movies") && busy[filepath.Dir(it.file)]) {
			errJSON(w, 400, "a job is still working on this")
			return
		}
		if err := lightFits(it.Info, b.Probe.Media); err != nil {
			errJSON(w, 400, err.Error())
			return
		}
		j, err := s.e.NewJob(b.Probe, lightPlanFor(it, b.Probe))
		if err != nil {
			errJSON(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, j)
	})
	mux.HandleFunc("POST /api/manager/light/delete", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ ID string }
		if !readJSON(w, r, &b) {
			return
		}
		it := s.p.c.Get(b.ID)
		if it == nil || it.Light == nil {
			errJSON(w, 400, "no Light copy")
			return
		}
		if s.busyFiles()[it.file] {
			errJSON(w, 400, "a job is still working on this")
			return
		}
		if err := os.Remove(it.Light.file); err != nil {
			errJSON(w, 500, err.Error())
			return
		}
		dropStream(it.Light.file)
		os.Remove(lightDir(it.file))
		s.p.c.Refresh()
		s.e.store.Notify()
		writeJSON(w, 200, map[string]any{"ok": true, "freed": it.Light.Size})
	})
}

// lightFits: what a Light copy has to be, checked on the link before it downloads.
func lightFits(film, light *MInfo) error {
	if light == nil {
		return nil // unreadable link: checked after the download instead
	}
	if !browserVideo(light) {
		return errors.New("a Light copy has to be H.264 or HEVC, 1080p at most and without HDR, to play in browsers")
	}
	if film != nil && film.Dur > 0 && light.Dur > 0 && !sameCut(film.Dur, light.Dur) {
		return errors.New("that file runs " + dur(int64(light.Dur)) + " and the film " + dur(int64(film.Dur)) + ": a different cut")
	}
	return nil
}

var probeTimeout = 20 * time.Second

// linkMedia reads a link's picture and sound details without downloading it.
func linkMedia(ctx context.Context, u string) *MInfo {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return probeMediaCtx(ctx, u)
}
