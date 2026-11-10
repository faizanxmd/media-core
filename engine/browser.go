package main

import (
	"net/http"
	"strings"
)

// What a browser can play, as it reports it: the web player asks the browser
// itself (player.js, browserCaps) and sends the answer as ?can=h264,hevc,...
// The engine decides with it, so Safari, which plays HEVC, HDR, Dolby Vision
// and Dolby audio and can switch audio tracks, gets the files as they are, and
// Chrome gets what it can play. No answer means an older page: the fixed rule
// in mp4.go (playsInBrowser) still applies.
//
// Tokens: h264, h264-10, hevc, av1, vp9 (picture); hdr (HDR10, HDR10+, HLG),
// dv (Dolby Vision); aac, mp3, opus, flac, ac3, eac3, alac (sound, ffprobe's
// names); switch (can change audio tracks, so any playable track will do).
type Caps struct {
	known bool
	set   map[string]bool
}

func parseCaps(s string) Caps {
	c := Caps{set: map[string]bool{}}
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(strings.ToLower(t)); t != "" {
			c.set[t], c.known = true, true
		}
	}
	return c
}

func capsOf(r *http.Request) Caps { return parseCaps(r.URL.Query().Get("can")) }

func (c Caps) has(t string) bool { return c.set[t] }

// playsIn: the file plays in this browser as it is.
func playsIn(m *MInfo, c Caps) bool {
	if !c.known {
		return playsInBrowser(m)
	}
	if m == nil || m.Container != "mp4" && m.Container != "m4v" {
		return false
	}
	return c.playsVideo(m) && (len(c.playableAudio(m.ACodecs, m.ACodec)) > 0 || len(m.ACodecs) == 0 && m.ACodec == "")
}

// playsVideo: this browser decodes the file's picture, its HDR or Dolby Vision
// included, whatever the file is wrapped in.
func (c Caps) playsVideo(m *MInfo) bool {
	if m == nil {
		return false
	}
	v := ""
	switch {
	case strings.HasPrefix(m.VCodec, "H.264"):
		v = "h264"
		if m.VBits > 8 {
			v = "h264-10"
		}
	case strings.HasPrefix(m.VCodec, "HEVC"):
		v = "hevc"
	case m.VCodec == "AV1":
		v = "av1"
	case m.VCodec == "VP9":
		v = "vp9"
	}
	if v == "" || !c.has(v) {
		return false
	}
	switch hdr := m.HDR; {
	case hdr == "":
	case strings.HasPrefix(hdr, "Dolby Vision"):
		// 8.x carries HDR10 or HLG underneath, which an HDR browser shows; 5 is Dolby Vision only
		if !c.has("dv") && !(strings.HasPrefix(hdr, "Dolby Vision 8") && c.has("hdr")) {
			return false
		}
	default:
		if !c.has("hdr") {
			return false
		}
	}
	return true
}

// playableAudio: the indexes of the tracks this browser can play. Without
// track switching only the first track can be heard, so only it counts.
func (c Caps) playableAudio(codecs []string, first string) []int {
	if len(codecs) == 0 && first != "" {
		codecs = []string{first}
	}
	var out []int
	for i, a := range codecs {
		if i > 0 && !c.has("switch") {
			break
		}
		if c.has(a) {
			out = append(out, i)
		}
	}
	return out
}
