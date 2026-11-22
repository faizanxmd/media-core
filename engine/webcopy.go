package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// What the web player plays (docs/browser-playback.md): a library file or its
// Light copy directly, or repackaged on the fly as HLS (stream.go).

// webManifest tells the web player how to play an item: stream URL or HLS
// playlist, audio tracks in the file's order, and available subtitles.
type webManifest struct {
	State    string              `json:"state"`
	Src      string              `json:"src,omitempty"`       // played directly: the library file or its Light copy
	HLS      string              `json:"hls,omitempty"`       // repackaged while it plays (stream.go): the stream's master playlist
	Wait     string              `json:"wait,omitempty"`      // not yet: what the server is doing first
	Light    bool                `json:"light,omitempty"`     // it's the title's Light copy that plays
	On       map[string]OnDevice `json:"on,omitempty"`        // this browser, the TV and the phone: plays or not, and why (play.go)
	SubsWait bool                `json:"subs_wait,omitempty"` // the file's own subtitles are still being read out (subs.go)
	Audio    []webAudio          `json:"audio,omitempty"`
	Subs     []webSub            `json:"subs,omitempty"`
}

type webAudio struct {
	Lang     string `json:"lang,omitempty"`
	Title    string `json:"title,omitempty"`
	Codec    string `json:"codec,omitempty"`
	Channels int    `json:"channels,omitempty"`
	Name     string `json:"name,omitempty"`   // what every player calls it: "English"
	Detail   string `json:"detail,omitempty"` // "DTS-HD MA 5.1", or "Commentary"
	Main     bool   `json:"main,omitempty"`   // the one to start with when nothing was chosen before
	Plays    bool   `json:"plays"`            // this browser can play it (by what it reported)
}

type webSub struct {
	File   string `json:"file"` // .vtt, or .sup for image subtitles
	Lang   string `json:"lang,omitempty"`
	Title  string `json:"title,omitempty"`
	Forced bool   `json:"forced,omitempty"`
}

// webPlay decides how this browser gets an item (play.go) and which file it is.
// want "light" asks for the Light copy when it has one that plays here: the
// player does that on a connection too slow for the film itself.
func webPlay(it *Item, c Caps, want ...string) (p Play, file string, info *MInfo) {
	if it == nil || it.file == "" {
		return Play{How: "none"}, "", nil
	}
	var light *MInfo
	if it.Light != nil {
		light = it.Light.Info
	}
	if len(want) > 0 && want[0] == "light" && light != nil {
		if p := howToPlay(light, nil, c); p.How != "none" {
			p.Light = true
			return p, it.Light.file, light
		}
	}
	p = howToPlay(it.Info, light, c)
	if p.Light {
		return p, it.Light.file, it.Light.Info
	}
	return p, it.file, it.Info
}

// directManifest is what the web player needs to play an item without a copy
// made ahead: where it is (the file as it is, or its stream), its sound tracks
// in the file's order, and the subtitle files beside it: text ones as
// x<n>.vtt, pictures as p<n>.sup.
func directManifest(it *Item, c Caps, want ...string) *webManifest {
	p, file, _ := webPlay(it, c, want...)
	m := &webManifest{State: "ready", Light: p.Light}
	switch p.How {
	case "direct":
		m.Src = "/api/v1/stream/" + it.ID
		if p.Light {
			m.Src += "?v=light"
		}
	case "repack":
		if _, err := keyframes(file); err == errKeysPending {
			return &webManifest{State: "wait", Wait: "The server is reading this file for the first time. It takes a few minutes."}
		} else if err != nil {
			return nil
		}
		m.HLS = streamURL(it, p.Light, c)
	default:
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if streams, err := probeStreamsKept(ctx, file); err == nil {
		var codecs []string
		var tracks []probeStream
		for _, s := range streams {
			if s.Type == "audio" {
				a := webAudio{Lang: s.Tags.Language, Title: s.Tags.Title, Codec: s.Codec, Channels: s.Channels, Plays: p.How == "repack",
					Name: trackName(s), Detail: audioLabel(s.Codec, s.Profile, s.Channels)}
				if isCommentary(s.Tags.Title) {
					a.Name, a.Detail = strings.TrimSuffix(a.Name, " commentary"), "Commentary"
				}
				m.Audio = append(m.Audio, a)
				codecs = append(codecs, s.Codec)
				tracks = append(tracks, s)
			}
		}
		if len(tracks) > 0 {
			m.Audio[mainAudio(tracks)].Main = true
		}
		if p.How == "direct" {
			if !c.known { // an older page: only the first track, if a browser plays it
				c = parseCaps("aac,mp3,opus,flac")
			}
			for _, i := range c.playableAudio(codecs, "") {
				m.Audio[i].Plays = true
			}
		}
	}
	for i, s := range it.Subs {
		if s.Mime == "application/x-subrip" || s.Mime == "text/vtt" {
			m.Subs = append(m.Subs, webSub{File: fmt.Sprintf("x%d.vtt", i), Lang: s.Lang, Title: s.Label, Forced: s.Forced})
		}
	}
	embedded, pending := embeddedSubs(it) // the ones inside the file, once they've been read out
	m.SubsWait = pending
	kind := func(e embSub) string { // the same words as text and as pictures: the text is enough
		return fmt.Sprint(e.Lang, e.Forced, strings.Contains(strings.ToLower(e.Title), "sdh"))
	}
	asText := map[string]bool{}
	for _, e := range embedded {
		if e.Kind == "vtt" {
			asText[kind(e)] = true
		}
	}
	for _, e := range embedded {
		if e.Kind == "sup" && asText[kind(e)] {
			continue
		}
		m.Subs = append(m.Subs, webSub{File: fmt.Sprintf("e%d.%s", e.Index, e.Kind), Lang: e.Lang, Title: e.Title, Forced: e.Forced})
	}
	for i, f := range supSidecars(it.file) {
		lang, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(filepath.Base(f), baseName(it.file)), "."), ".")
		m.Subs = append(m.Subs, webSub{File: fmt.Sprintf("p%d.sup", i), Lang: lang, Forced: strings.Contains(filepath.Base(f), ".forced.")})
	}
	return m
}

// supSidecars: picture subtitle sidecars (.sup) beside a video.
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
	mountStreams(mux, p)
	// which items play in this browser, for a title page's episodes and the player's queue
	mux.HandleFunc("GET /api/v1/web", func(w http.ResponseWriter, r *http.Request) {
		p.c.WaitReady(r.Context(), 5*time.Second)
		c := capsOf(r)
		ready := map[string]bool{}
		blocked := map[string]string{} // what this browser can't play, and why
		for _, it := range p.c.All() {
			if it.file == "" {
				continue
			}
			if p, _, _ := webPlay(it, c); p.How != "none" {
				ready[it.ID] = true
			} else {
				blocked[it.ID] = c.whyNot(it.Info)
			}
		}
		writeJSON(w, 200, map[string]any{"ready": ready, "blocked": blocked})
	})
	mux.HandleFunc("GET /api/v1/web/{id}", func(w http.ResponseWriter, r *http.Request) {
		p.c.WaitReady(r.Context(), 5*time.Second)
		it := p.c.Get(r.PathValue("id"))
		if it == nil {
			errJSON(w, 404, "not in the library (rescanned?)")
			return
		}
		var light *MInfo
		if it.Light != nil {
			light = it.Light.Info
		}
		on := onDevices(it.Info, light, capsOf(r))
		if m := directManifest(it, capsOf(r), r.URL.Query().Get("v")); m != nil {
			m.On = on
			writeJSON(w, 200, m)
			return
		}
		writeJSON(w, 200, webManifest{State: "none", On: on})
	})
	mux.HandleFunc("GET /api/v1/web/{id}/{file}", func(w http.ResponseWriter, r *http.Request) {
		it := p.c.Get(r.PathValue("id"))
		name := r.PathValue("file")
		if it != nil && it.file != "" { // subtitles from inside the file, or beside it
			if rest, ok := strings.CutPrefix(name, "e"); ok && (strings.HasSuffix(rest, ".vtt") || strings.HasSuffix(rest, ".sup")) {
				if _, err := strconv.Atoi(rest[:len(rest)-4]); err == nil {
					if strings.HasSuffix(rest, ".vtt") {
						w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
					} else {
						w.Header().Set("Content-Type", "application/octet-stream")
					}
					http.ServeFile(w, r, filepath.Join(subsDir(it.file), name))
					return
				}
			}
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
		http.NotFound(w, r)
	})
}
