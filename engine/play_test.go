package main

import (
	"strings"
	"testing"
)

func TestHowToPlay(t *testing.T) {
	// what the user's browsers report (mint's journal, 2026-11-10)
	safari := parseCaps("h264,hevc,hdr,dv,aac,mp3,opus,flac,ac3,eac3,alac,switch")
	chrome := parseCaps("h264,h264-10,hevc,av1,vp9,hdr,aac,mp3,opus,flac")
	f := func(box, vc, hdr string, audio ...string) *MInfo {
		m := &MInfo{Container: box, VCodec: vc, VBits: 8, HDR: hdr, ACodecs: audio}
		if strings.Contains(vc, "10") {
			m.VBits = 10
		}
		if len(audio) > 0 {
			m.ACodec = audio[0]
		}
		return m
	}
	light := f("mp4", "H.264", "", "aac")
	cases := []struct {
		name           string
		m, light       *MInfo
		safari, chrome string // how, then the sound tracks when repackaged
	}{
		{"MP4, H.264, AAC", f("mp4", "H.264", "", "aac"), nil, "direct", "direct"},
		{"MP4, HEVC HDR10, DD+ Atmos", f("mp4", "HEVC Main 10", "HDR10", "eac3"), nil, "direct", "repack aac"},
		{"MP4, DD+ then AAC", f("mp4", "H.264", "", "eac3", "aac"), nil, "direct", "repack aac copy"},
		{"MKV, H.264, AAC", f("mkv", "H.264", "", "aac"), nil, "repack copy", "repack copy"},
		{"MKV, HEVC, DD+", f("mkv", "HEVC Main 10", "", "eac3"), nil, "repack copy", "repack aac"},
		{"MKV, 4K Dolby Vision 8.1, DTS-HD MA", f("mkv", "HEVC Main 10", "Dolby Vision 8.1", "dts"), nil, "repack aac", "repack aac"},
		{"MKV, TrueHD then DD", f("mkv", "HEVC Main 10", "HDR10", "truehd", "ac3"), nil, "repack aac copy", "repack aac aac"},
		{"MKV, Opus", f("mkv", "H.264", "", "opus"), nil, "repack copy", "repack copy"},
		{"MKV, no sound", f("mkv", "H.264", ""), nil, "repack", "repack"},
		{"Dolby Vision 5, no Light copy", f("mp4", "HEVC Main 10", "Dolby Vision 5", "eac3"), nil, "direct", "none"},
		{"Dolby Vision 5, Light copy", f("mp4", "HEVC Main 10", "Dolby Vision 5", "eac3"), light, "direct", "light direct"},
		{"AV1, no Light copy", f("mkv", "AV1", "", "opus"), nil, "none", "repack copy"},
		{"AV1, Light copy", f("mkv", "AV1", "", "opus"), light, "light direct", "repack copy"},
		{"AV1, Light copy is an MKV with DD+", f("mkv", "AV1", "", "opus"), f("mkv", "H.264", "", "eac3"), "light repack copy", "repack copy"},
		{"H.264 10-bit", f("mkv", "H.264 High 10", "", "aac"), nil, "none", "repack copy"},
		{"MPEG-2", f("mkv", "MPEG-2", "", "ac3"), nil, "none", "none"},
	}
	say := func(p Play) string {
		s := p.How
		if p.Light {
			s = "light " + s
		}
		if len(p.Audio) > 0 {
			s += " " + strings.Join(p.Audio, " ")
		}
		return s
	}
	for _, c := range cases {
		if got := say(howToPlay(c.m, c.light, safari)); got != c.safari {
			t.Errorf("%s: Safari %q, want %q", c.name, got, c.safari)
		}
		if got := say(howToPlay(c.m, c.light, chrome)); got != c.chrome {
			t.Errorf("%s: Chrome %q, want %q", c.name, got, c.chrome)
		}
	}
	// nothing plays: it says why
	if p := howToPlay(f("mkv", "MPEG-2", "", "ac3"), nil, safari); p.Why == "" {
		t.Error("none: no reason given")
	}
	// an older page that sends nothing: the fixed rule, never a stream
	if got := say(howToPlay(f("mkv", "H.264", "", "aac"), nil, Caps{})); got != "none" {
		t.Errorf("no caps, MKV: %q, want none", got)
	}
	if got := say(howToPlay(f("mkv", "H.264", "", "aac"), light, Caps{})); got != "light direct" {
		t.Errorf("no caps, MKV with a Light copy: %q, want light direct", got)
	}
	// which devices: a Dolby Vision 5 film plays on the TV and in Safari, not on the phone or in Chrome
	on := onDevices(f("mkv", "HEVC Main 10", "Dolby Vision 5", "eac3"), nil, chrome)
	if !on["tv"].OK || on["phone"].OK || on["here"].OK || !strings.Contains(on["phone"].Why, "Dolby Vision 5") {
		t.Errorf("Dolby Vision 5: %+v", on)
	}
	if on := onDevices(f("mkv", "HEVC Main 10", "Dolby Vision 5", "eac3"), light, chrome); !on["phone"].OK || on["phone"].How != "light" || on["here"].How != "light" {
		t.Errorf("Dolby Vision 5 with a Light copy: %+v", on)
	}
	if on := onDevices(f("mkv", "H.264 High 10", "", "aac"), nil, safari); on["tv"].OK || on["tv"].Why != "10-bit H.264" || on["here"].OK {
		t.Errorf("10-bit H.264: %+v", on)
	}
	if on := onDevices(f("mkv", "HEVC Main 10", "Dolby Vision 8.1", "dts"), nil, safari); !on["tv"].OK || !on["phone"].OK || on["here"].How != "repack" {
		t.Errorf("Dolby Vision 8.1 MKV: %+v", on)
	}
	if got := say(howToPlay(nil, nil, safari)); got != "none" {
		t.Errorf("unread file: %q, want none", got)
	}
}
