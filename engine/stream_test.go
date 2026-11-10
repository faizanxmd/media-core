package main

import (
	"strings"
	"testing"
)

func TestCutTimes(t *testing.T) {
	// keyframes every 2.002 s after two at the very start, as a 4K WEB-DL has them
	keys := []float64{0, 0.083}
	for k := 2.085; k < 60; k += 2.002 {
		keys = append(keys, k)
	}
	cuts := cutTimes(keys, 60, 2)
	if cuts[0] != 0 || cuts[1] != 2.085 {
		t.Errorf("starts %v: the keyframe at 0.083 should fold into the first piece", cuts[:2])
	}
	for i := 1; i < len(cuts); i++ {
		if d := cuts[i] - cuts[i-1]; d < 1.999 {
			t.Errorf("piece %d is %.3f s, shorter than asked", i-1, d)
		}
	}
	if last := cuts[len(cuts)-1]; last >= 59.5 {
		t.Errorf("last piece starts at %.3f: a sliver at the end", last)
	}
	// long gaps stay whole, short ones are grouped
	cuts = cutTimes([]float64{0, 1, 2, 3, 13, 13.5, 20}, 30, 4)
	if want := []float64{0, 13, 20}; len(cuts) != len(want) || cuts[1] != 13 || cuts[2] != 20 {
		t.Errorf("cuts %v, want %v", cuts, want)
	}
}

func TestPieceAt(t *testing.T) {
	s := &liveStream{dur: 30, cuts: []float64{0, 2.085, 4.087, 6.089}}
	v := &liveTrack{s: s, name: "v"}
	for at, want := range map[float64]int{0: 0, 0.083: 0, 2.084: 1, 2.085: 1, 4.0: 1, 4.087: 2, 29: 3, -0.5: 0} {
		if got := v.pieceAt(at); got != want {
			t.Errorf("picture at %.3f: piece %d, want %d", at, got, want)
		}
	}
	a := &liveTrack{s: s, name: "a0t"}
	for at, want := range map[float64]int{-0.021: 0, 0: 0, 2.046: 0, 2.0475: 1, 2.048: 1, 4.0955: 2, 100: 14} {
		if got := a.pieceAt(at); got != want {
			t.Errorf("sound at %.4f: piece %d, want %d", at, got, want)
		}
	}
	if n := a.count(); n != 15 {
		t.Errorf("30 s of sound: %d pieces, want 15", n)
	}
	if _, l := v.span(3); l < 23.9 || l > 23.92 {
		t.Errorf("last picture piece runs %.3f s, want to the end of the film", l)
	}
}

func TestAudioTrackName(t *testing.T) {
	for name, want := range map[string]string{"a0c": "0c", "a12t": "12t", "v": "", "a0": "", "a0x": "", "ac": "", "a-1t": ""} {
		n, mode, ok := audioTrackName(name)
		got := ""
		if ok {
			got = itoa(n) + string(mode)
		}
		if got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}

func TestMasterPlaylist(t *testing.T) {
	tr := func(codec, lang, title string, ch, def int) probeStream {
		p := probeStream{Type: "audio", Codec: codec, Channels: ch}
		p.Tags.Language, p.Tags.Title, p.Disp.Default = lang, title, def
		return p
	}
	// a foreign release: the dub first and marked default, English second, a commentary third
	s := &liveStream{vcodec: "hevc", audio: []probeStream{tr("eac3", "hin", "", 2, 1), tr("dts", "eng", "", 6, 0), tr("ac3", "eng", "Director's commentary", 2, 0)}}
	m := &MInfo{Res: "4K", VBits: 10, HDR: "Dolby Vision 8.1", Width: 3840, Height: 2160, FPS: 23.976, Mbps: 29.2}
	safari := s.masterPlaylist(m, parseCaps("hevc,hdr,dv,aac,ac3,eac3,switch"))
	for _, want := range []string{`NAME="Hindi",LANGUAGE="hin",DEFAULT=NO`, `URI="a0c.m3u8"`, `NAME="English",LANGUAGE="eng",DEFAULT=YES`, `URI="a1t.m3u8"`,
		`NAME="English commentary"`, `URI="a2c.m3u8"`, `CODECS="hvc1.2.4.L153.B0,ec-3,mp4a.40.2,ac-3"`, "VIDEO-RANGE=PQ", "RESOLUTION=3840x2160"} {
		if !strings.Contains(safari, want) {
			t.Errorf("Safari's playlist lacks %s:\n%s", want, safari)
		}
	}
	chrome := s.masterPlaylist(m, parseCaps("hevc,hdr,aac,opus,flac"))
	if strings.Contains(chrome, "ec-3") || strings.Contains(chrome, "ac-3") || !strings.Contains(chrome, `URI="a0t.m3u8"`) || !strings.Contains(chrome, `URI="a2t.m3u8"`) {
		t.Errorf("Chrome should get every track as AAC:\n%s", chrome)
	}
	if got := videoCodecName(&MInfo{HDR: "Dolby Vision 5", VBits: 10}, "hevc"); got != "dvh1.05.06" {
		t.Errorf("Dolby Vision 5 is named %q", got)
	}
}

func TestEBMLHead(t *testing.T) {
	// a Cues id (4 bytes) with a 2-byte size of 0x1234, then an unknown-size Segment
	id, size, n, ok := ebmlHead([]byte{0x1C, 0x53, 0xBB, 0x6B, 0x52, 0x34, 0, 0})
	if !ok || id != mkvCues || size != 0x1234 || n != 6 {
		t.Errorf("cues head: id %x size %x n %d ok %v", id, size, n, ok)
	}
	if _, size, _, ok := ebmlHead([]byte{0x18, 0x53, 0x80, 0x67, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}); !ok || size != -1 {
		t.Errorf("unknown size: %d, %v", size, ok)
	}
}
