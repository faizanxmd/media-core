package main

import (
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Browser copies: what the web player plays (docs/web-copies.md). This file only
// finds and serves them; each lives in its own folder under DataDir/web, named
// after the item's id, and a copy counts once its manifest.json is there.

// webManifest is a copy's manifest.json. The track lists are in the playlists'
// and files' order, with the source's own language and title, so the player
// can name them.
type webManifest struct {
	State       string     `json:"state"`
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
	return &m
}

func mountWebCopies(mux *http.ServeMux, p *Player) {
	// which items have a copy, for a title page's episodes and the player's queue
	mux.HandleFunc("GET /api/v1/web", func(w http.ResponseWriter, r *http.Request) {
		ready := map[string]bool{}
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
		if m := webCopy(it); m != nil {
			writeJSON(w, 200, m)
			return
		}
		writeJSON(w, 200, webManifest{State: "none"})
	})
	mux.HandleFunc("GET /api/v1/web/{id}/{file}", func(w http.ResponseWriter, r *http.Request) {
		it := p.c.Get(r.PathValue("id"))
		name := r.PathValue("file")
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
		if strings.HasSuffix(name, ".m4s") { // eases downloads and pauses background work, as the apps' streams do
			touchPlaying(r, it.ID)
		}
		http.ServeFile(w, r, filepath.Join(webDir(it.ID), name))
	})
}
