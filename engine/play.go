package main

// How a browser gets a title, cheapest first. The file is never rewritten and
// the picture is never converted while it plays (mint's CPU can't):
//
//	direct  the file as it is, with byte ranges
//	repack  the picture copied untouched into a stream made while it plays,
//	        each sound track copied when this browser plays it, else made AAC
//	light   the title's Light copy, when this browser can't decode the
//	        picture itself (Dolby Vision 5 in Chrome, AV1 in Safari)
//	none    nothing plays here; Why says what would help
//
// Tested on the user's Mac (2026-11-10): Safari opens no MKV at all but plays
// a repackaged 4K Dolby Vision stream; Chrome opens MKV but has no Dolby or
// DTS sound in any container.
type Play struct {
	How   string   `json:"how"`             // direct | repack | light | none
	Light bool     `json:"light,omitempty"` // it's the Light copy that plays
	Audio []string `json:"audio,omitempty"` // repack: per sound track, "copy" or "aac"
	Why   string   `json:"why,omitempty"`   // none: what's in the way
}

// streamAudio can go into a stream's pieces as it is; anything else becomes AAC there.
var streamAudio = set("aac", "mp3", "opus", "flac", "ac3", "eac3", "alac")

// howToPlay decides for one file. light is the title's Light copy, or nil.
func howToPlay(m, light *MInfo, c Caps) Play {
	if m == nil {
		return Play{How: "none", Why: "The file hasn't been read yet."}
	}
	if !c.known { // an older page: the fixed rule, and no streams
		switch {
		case playsInBrowser(m):
			return Play{How: "direct"}
		case playsInBrowser(light):
			return Play{How: "direct", Light: true}
		}
		return Play{How: "none", Why: "This page is out of date. Reload it."}
	}
	if p, ok := c.playOne(m); ok {
		return p
	}
	if p, ok := c.playOne(light); ok {
		p.Light = true
		return p
	}
	if light != nil {
		return Play{How: "none", Why: "This browser can't decode the picture, and not its Light copy either."}
	}
	return Play{How: "none", Why: "This browser can't decode the picture. A Light copy would play."}
}

// playOne: one file, direct when it plays as it is, else repackaged when the
// picture is one this browser decodes.
func (c Caps) playOne(m *MInfo) (Play, bool) {
	if m == nil || !c.playsVideo(m) {
		return Play{}, false
	}
	if playsIn(m, c) {
		return Play{How: "direct"}, true
	}
	codecs := m.ACodecs
	if len(codecs) == 0 && m.ACodec != "" {
		codecs = []string{m.ACodec}
	}
	p := Play{How: "repack"}
	for _, a := range codecs {
		if c.has(a) && streamAudio[a] {
			p.Audio = append(p.Audio, "copy")
		} else {
			p.Audio = append(p.Audio, "aac")
		}
	}
	return p, true
}
