package main

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// What the web player plays (docs/browser-playback.md): a library file or its Light copy
// directly, else an old HLS browser copy. A copy lives in its own folder under
// DataDir/web, named after the item's id, and counts once its manifest.json is there.

// webManifest is a copy's manifest.json. The track lists are in the playlists'
// and files' order, with the source's own language and title, so the player
// can name them.
type webManifest struct {
	State       string     `json:"state"`
	Src         string     `json:"src,omitempty"` // played directly: the library file or its Light copy
	SourceSize  int64      `json:"source_size"`
	SourceMtime int64      `json:"source_mtime"`
	Audio       []webAudio `json:"audio,omitempty"`
	Subs        []webSub   `json:"subs,omitempty"`
}

type webAudio struct {
	Lang     string `json:"lang,omitempty"`
	Title    string `json:"title,omitempty"`
	Codec    string `json:"codec,omitempty"`
	Channels int    `json:"channels,omitempty"`
	Plays    bool   `json:"plays"` // this browser can play it (by what it reported)
}

type webSub struct {
	File   string `json:"file"` // .vtt, or .sup for image subtitles
	Lang   string `json:"lang,omitempty"`
	Title  string `json:"title,omitempty"`
	Forced bool   `json:"forced,omitempty"`
}

func webDir(id string) string { return filepath.Join(DataDir, "web", id) }

// webCopy reads an item's copy. A copy made from a file since replaced doesn't count.
func webCopy(it *Item) *webManifest {
	if it == nil || it.file == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(webDir(it.ID), "manifest.json"))
	if err != nil {
		return nil
	}
	var m webManifest
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	size, mtime, ok := fileKey(it.file)
	if !ok || size != m.SourceSize || mtime != m.SourceMtime {
		return nil
	}
	m.State = "ready"
	for i := range m.Audio { // a copy's sound was made for browsers (AAC)
		m.Audio[i].Plays = true
	}
	return &m
}

// webDirect is how a browser plays an item without a copy: the file itself
// when this browser plays it, else its Light copy when that plays (browser.go).
func webDirect(it *Item, c Caps) (src, file string) {
	switch {
	case it == nil || it.file == "":
	case playsIn(it.Info, c):
		return "/api/v1/stream/" + it.ID, it.file
	case it.Light != nil && playsIn(it.Light.Info, c):
		return "/api/v1/stream/" + it.ID + "?v=light", it.Light.file
	}
	return "", ""
}

// directManifest lists a directly played file's audio (from the file, in its
// order) and the subtitle files beside it: text ones as x<n>.vtt, pictures as p<n>.sup.
func directManifest(it *Item, c Caps) *webManifest {
	src, file := webDirect(it, c)
	if src == "" {
		return nil
	}
	m := &webManifest{State: "ready", Src: src}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if streams, err := probeStreams(ctx, file); err == nil {
		var codecs []string
		for _, s := range streams {
			if s.Type == "audio" {
				m.Audio = append(m.Audio, webAudio{Lang: s.Tags.Language, Title: s.Tags.Title, Codec: s.Codec, Channels: s.Channels})
				codecs = append(codecs, s.Codec)
			}
		}
		if !c.known { // an older page: only the first track, if a browser plays it
			c = parseCaps("aac,mp3,opus,flac")
		}
		for _, i := range c.playableAudio(codecs, "") {
			m.Audio[i].Plays = true
		}
	}
	for i, s := range it.Subs {
		if s.Mime == "application/x-subrip" || s.Mime == "text/vtt" {
			m.Subs = append(m.Subs, webSub{File: fmt.Sprintf("x%d.vtt", i), Lang: s.Lang, Title: s.Label, Forced: s.Forced})
		}
	}
	for i, f := range supSidecars(it.file) {
		lang, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(filepath.Base(f), baseName(it.file)), "."), ".")
		m.Subs = append(m.Subs, webSub{File: fmt.Sprintf("p%d.sup", i), Lang: lang, Forced: strings.Contains(filepath.Base(f), ".forced.")})
	}
	return m
}

// supSidecars: picture subtitles saved beside a video by the MP4 step.
func supSidecars(video string) []string {
	var out []string
	ents, _ := os.ReadDir(filepath.Dir(video))
	for _, e := range ents {
		if ext(e.Name()) == "sup" && strings.HasPrefix(e.Name(), baseName(video)+".") {
			out = append(out, filepath.Join(filepath.Dir(video), e.Name()))
		}
	}
	return out
}

var reSrtTime = regexp.MustCompile(`(\d{2}:\d{2}:\d{2}),(\d{3})`)

// srtToVTT: browsers read WebVTT; an .srt only differs in its header and commas.
func srtToVTT(b []byte) []byte {
	s := strings.ReplaceAll(strings.TrimPrefix(string(b), "\ufeff"), "\r", "")
	if strings.HasPrefix(s, "WEBVTT") {
		return []byte(s)
	}
	return []byte("WEBVTT\n\n" + reSrtTime.ReplaceAllString(s, "$1.$2"))
}

func mountWebCopies(mux *http.ServeMux, p *Player) {
	// which items have a copy, for a title page's episodes and the player's queue
	mux.HandleFunc("GET /api/v1/web", func(w http.ResponseWriter, r *http.Request) {
		c := capsOf(r)
		ready := map[string]bool{}
		for _, it := range p.c.All() {
			if src, _ := webDirect(it, c); src != "" {
				ready[it.ID] = true
			}
		}
		ents, _ := os.ReadDir(filepath.Join(DataDir, "web"))
		for _, e := range ents {
			if webCopy(p.c.Get(e.Name())) != nil {
				ready[e.Name()] = true
			}
		}
		writeJSON(w, 200, map[string]any{"ready": ready})
	})
	mux.HandleFunc("GET /api/v1/web/{id}", func(w http.ResponseWriter, r *http.Request) {
		it := p.c.Get(r.PathValue("id"))
		if it == nil {
			errJSON(w, 404, "not in the library (rescanned?)")
			return
		}
		if m := directManifest(it, capsOf(r)); m != nil { // the original's own picture beats a copy
			writeJSON(w, 200, m)
			return
		}
		if m := webCopy(it); m != nil {
			writeJSON(w, 200, m)
			return
		}
		writeJSON(w, 200, webManifest{State: "none"})
	})
	mux.HandleFunc("GET /api/v1/web/{id}/{file}", func(w http.ResponseWriter, r *http.Request) {
		it := p.c.Get(r.PathValue("id"))
		name := r.PathValue("file")
		copied := webCopy(it) != nil && filepath.IsLocal(name) && !strings.ContainsAny(name, `/\`) && exists(filepath.Join(webDir(it.ID), name))
		if it != nil && it.file != "" && !copied { // subtitles beside a directly played file
			var n int
			if _, err := fmt.Sscanf(name, "x%d.vtt", &n); err == nil && n >= 0 && n < len(it.Subs) {
				b, err := os.ReadFile(it.Subs[n].file)
				if err != nil {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
				w.Write(srtToVTT(b))
				return
			}
			if _, err := fmt.Sscanf(name, "p%d.sup", &n); err == nil {
				if sups := supSidecars(it.file); n >= 0 && n < len(sups) {
					w.Header().Set("Content-Type", "application/octet-stream")
					http.ServeFile(w, r, sups[n])
					return
				}
			}
		}
		if webCopy(it) == nil || !filepath.IsLocal(name) || strings.ContainsAny(name, `/\`) {
			http.NotFound(w, r)
			return
		}
		ctype := map[string]string{".m3u8": "application/vnd.apple.mpegurl", ".m4s": "video/mp4", ".vtt": "text/vtt; charset=utf-8", ".sup": "application/octet-stream"}[filepath.Ext(name)]
		if ctype == "" {
			ctype = mime.TypeByExtension(filepath.Ext(name))
		}
		if ctype != "" {
			w.Header().Set("Content-Type", ctype)
		}
		if strings.HasSuffix(name, ".m4s") { // pauses background work, as the apps' streams do
			touchPlaying(r, it.ID)
		}
		http.ServeFile(w, r, filepath.Join(webDir(it.ID), name))
	})
}
