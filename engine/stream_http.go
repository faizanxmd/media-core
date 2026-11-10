package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// The stream's addresses, on :8780 only (stream.go makes the pieces):
//
//	/api/v1/hls/{id}/{which}/master.m3u8?can=…   the picture and every sound track, for this browser
//	/api/v1/hls/{id}/{which}/v.m3u8              the picture's pieces
//	/api/v1/hls/{id}/{which}/a0t.m3u8            sound track 0 made AAC ("a0c": copied as it is)
//	/api/v1/hls/{id}/{which}/v-init.mp4, v-12.m4s, a0t-init.mp4, a0t-12.m4s
//
// which is "o" for the file or "l" for its Light copy, then the file's change
// time, so a browser's kept pieces never outlive the file they came from.

func streamWhich(light bool, file string) string {
	_, mtime, _ := fileKey(file)
	w := "o"
	if light {
		w = "l"
	}
	return w + strconv.FormatInt(mtime, 36)
}

// streamURL: where a browser finds the stream of an item's file or Light copy.
func streamURL(it *Item, light bool, c Caps) string {
	file := it.file
	if light {
		file = it.Light.file
	}
	return "/api/v1/hls/" + it.ID + "/" + streamWhich(light, file) + "/master.m3u8?can=" + url.QueryEscape(c.String())
}

func (c Caps) String() string {
	var out []string
	for t := range c.set {
		out = append(out, t)
	}
	return strings.Join(out, ",")
}

var streamCodec = map[string]string{"aac": "mp4a.40.2", "mp3": "mp4a.40.34", "ac3": "ac-3", "eac3": "ec-3", "flac": "fLaC", "opus": "Opus", "alac": "alac"}

// videoCodecName: the picture's name in a playlist. Browsers check it before
// loading anything, so it has to be one they know; the level is a safe ceiling.
func videoCodecName(m *MInfo, codec string) string {
	big, ten := m.Res == "4K", m.VBits > 8
	switch codec {
	case "hevc":
		level := "L123"
		if big {
			level = "L153"
		}
		switch {
		case strings.HasPrefix(m.HDR, "Dolby Vision 5"):
			return "dvh1.05.06"
		case ten:
			return "hvc1.2.4." + level + ".B0"
		}
		return "hvc1.1.6." + level + ".B0"
	case "h264":
		level := "29"
		if big {
			level = "33"
		}
		if ten {
			return "avc1.6e00" + level
		}
		return "avc1.6400" + level
	case "av1":
		if ten {
			return "av01.0.12M.10"
		}
		return "av01.0.12M.08"
	case "vp9":
		if ten {
			return "vp09.02.51.10"
		}
		return "vp09.00.51.08"
	}
	return ""
}

// masterPlaylist lists the picture and the sound tracks, each copied or made AAC as this browser needs.
func (s *liveStream) masterPlaylist(m *MInfo, c Caps) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	codecs := []string{videoCodecName(m, s.vcodec)}
	first := mainAudio(s.audio)
	names := map[string]int{}
	for i, a := range s.audio {
		mode, codec := "t", "aac"
		if c.has(a.Codec) && streamAudio[a.Codec] {
			mode, codec = "c", a.Codec
		}
		if cs := streamCodec[codec]; !strings.Contains(strings.Join(codecs, ","), cs) {
			codecs = append(codecs, cs)
		}
		name := trackName(a)
		if names[name]++; names[name] > 1 {
			name += " " + itoa(names[name])
		}
		ch := 2
		if mode == "c" && a.Channels > 0 {
			ch = a.Channels
		}
		def := "NO"
		if i == first {
			def = "YES"
		}
		lang := ""
		if l := strings.ToLower(a.Tags.Language); l != "" && l != "und" {
			lang = fmt.Sprintf(`LANGUAGE="%s",`, l)
		}
		fmt.Fprintf(&b, "#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"%s\",%sDEFAULT=%s,AUTOSELECT=YES,CHANNELS=\"%d\",URI=\"a%d%s.m3u8\"\n",
			strings.ReplaceAll(name, `"`, "'"), lang, def, ch, i, mode)
	}
	rate := m.Mbps * 1e6
	if rate <= 0 {
		rate = 8e6
	}
	fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,AVERAGE-BANDWIDTH=%d", int(rate*1.25), int(rate))
	if m.Width > 0 && m.Height > 0 {
		fmt.Fprintf(&b, ",RESOLUTION=%dx%d", m.Width, m.Height)
	}
	if m.FPS > 0 {
		fmt.Fprintf(&b, ",FRAME-RATE=%.3f", m.FPS)
	}
	fmt.Fprintf(&b, ",CODECS=\"%s\"", strings.Join(codecs, ","))
	switch {
	case m.HDR == "HLG":
		b.WriteString(",VIDEO-RANGE=HLG")
	case m.HDR != "":
		b.WriteString(",VIDEO-RANGE=PQ")
	default:
		b.WriteString(",VIDEO-RANGE=SDR")
	}
	if len(s.audio) > 0 {
		b.WriteString(`,AUDIO="a"`)
	}
	b.WriteString("\nv.m3u8\n")
	return b.String()
}

// mainAudio: the track a player starts with: English, then the file's default, else the first.
func mainAudio(audio []probeStream) int {
	best, rank := 0, -1
	for i, a := range audio {
		r := a.Disp.Default
		if isEnglish(a.Tags.Language) && !isCommentary(a.Tags.Title) {
			r += 2
		}
		if r > rank {
			best, rank = i, r
		}
	}
	return best
}

// trackName: a sound track's name in the stream: its language, or its title when it has none.
func trackName(a probeStream) string {
	if n := langName(a.Tags.Language); n != "" {
		if isCommentary(a.Tags.Title) {
			return n + " commentary"
		}
		return n
	}
	if t := strings.TrimSpace(strings.Split(a.Tags.Title, "|")[0]); t != "" {
		return t
	}
	return "Unknown"
}

// playlist lists one track's pieces.
func (t *liveTrack) playlist() string {
	var b strings.Builder
	longest := 0.0
	n := t.count()
	for i := 0; i < n; i++ {
		if _, l := t.span(i); l > longest {
			longest = l
		}
	}
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-INDEPENDENT-SEGMENTS\n#EXT-X-MAP:URI=\"%s-init.mp4\"\n", int(longest+0.999), t.name)
	for i := 0; i < n; i++ {
		_, l := t.span(i)
		fmt.Fprintf(&b, "#EXTINF:%.3f,\n%s-%d.m4s\n", l, t.name, i)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func mountStreams(mux *http.ServeMux, p *Player) {
	mux.HandleFunc("GET /api/v1/hls/{id}/{which}/{name}", func(w http.ResponseWriter, r *http.Request) {
		it := p.c.Get(r.PathValue("id"))
		which, name := r.PathValue("which"), r.PathValue("name")
		if it == nil || it.file == "" || which == "" {
			http.NotFound(w, r)
			return
		}
		file, info, light := it.file, it.Info, which[0] == 'l'
		if light {
			if it.Light == nil {
				http.NotFound(w, r)
				return
			}
			file, info = it.Light.file, it.Light.Info
		}
		if which != streamWhich(light, file) { // the file changed since this stream was handed out
			http.NotFound(w, r)
			return
		}
		serveStream(w, r, it.ID, file, info, name)
	})
}

// serveStream answers one request for a file's stream: a playlist, an init piece or a piece.
func serveStream(w http.ResponseWriter, r *http.Request, id, file string, info *MInfo, name string) {
	s, err := streamFor(file, info)
	if errors.Is(err, errKeysPending) {
		w.Header().Set("Retry-After", "10")
		http.Error(w, "still reading the file", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h := w.Header()
	text := func(body string) {
		h.Set("Content-Type", "application/vnd.apple.mpegurl")
		h.Set("Cache-Control", "no-store")
		w.Write([]byte(body))
	}
	if name == "master.m3u8" {
		text(s.masterPlaylist(info, capsOf(r)))
		return
	}
	if tn, ok := strings.CutSuffix(name, ".m3u8"); ok {
		if t := s.track(tn); t != nil {
			text(t.playlist())
			return
		}
		http.NotFound(w, r)
		return
	}
	tn, part, ok := strings.Cut(name, "-")
	t := s.track(tn)
	if !ok || t == nil {
		http.NotFound(w, r)
		return
	}
	var body []byte
	if part == "init.mp4" {
		body, err = t.header(r.Context())
	} else if n, e := strconv.Atoi(strings.TrimSuffix(part, ".m4s")); e == nil && strings.HasSuffix(part, ".m4s") {
		touchPlaying(r, id)
		body, err = t.piece(r.Context(), n)
	} else {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		if r.Context().Err() == nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	h.Set("Content-Type", "video/mp4")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	h.Set("Cache-Control", "private, max-age=86400") // the same bytes every time, until the file changes
	w.Write(body)
}
