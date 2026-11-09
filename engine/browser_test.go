package main

import "testing"

func TestPlaysIn(t *testing.T) {
	safari := parseCaps("h264,hevc,hdr,dv,aac,mp3,opus,flac,ac3,eac3,alac,switch")
	chrome := parseCaps("h264,h264-10,hevc,av1,vp9,aac,mp3,opus,flac")
	mp4 := func(vc, hdr, res string, audio ...string) *MInfo {
		m := &MInfo{Container: "mp4", VCodec: vc, VBits: 8, HDR: hdr, Res: res, ACodecs: audio}
		if len(audio) > 0 {
			m.ACodec = audio[0]
		}
		return m
	}
	cases := []struct {
		name           string
		m              *MInfo
		safari, chrome bool
	}{
		{"1080p H.264, AAC first", mp4("H.264", "", "1080p", "aac", "eac3"), true, true},
		{"1080p H.264, DD+ only", mp4("H.264", "", "1080p", "eac3"), true, false},
		{"French DD+ first, English DD+ second", mp4("H.264", "", "1080p", "eac3", "eac3"), true, false},
		{"4K HEVC HDR10, DD+ Atmos", mp4("HEVC Main 10", "HDR10", "4K", "eac3"), true, false},
		{"4K Dolby Vision 8.1, AAC", mp4("HEVC Main 10", "Dolby Vision 8.1", "4K", "aac"), true, false},
		{"Dolby Vision 5, AAC", mp4("HEVC Main 10", "Dolby Vision 5", "4K", "aac"), true, false},
		{"DTS only", mp4("HEVC", "", "1080p", "dts"), false, false},
		{"TrueHD first, AC-3 second", mp4("HEVC", "", "1080p", "truehd", "ac3"), true, false},
		{"AV1", mp4("AV1", "", "1080p", "opus"), false, true},
		{"no sound", mp4("H.264", "", "1080p"), true, true},
		{"MKV", &MInfo{Container: "mkv", VCodec: "H.264", VBits: 8, ACodecs: []string{"aac"}, ACodec: "aac"}, false, false},
	}
	for _, c := range cases {
		if got := playsIn(c.m, safari); got != c.safari {
			t.Errorf("%s: Safari %v, want %v", c.name, got, c.safari)
		}
		if got := playsIn(c.m, chrome); got != c.chrome {
			t.Errorf("%s: Chrome %v, want %v", c.name, got, c.chrome)
		}
	}
	// an older page that sends nothing keeps the fixed rule: 1080p, no HDR, browser audio first
	if playsIn(mp4("HEVC Main 10", "HDR10", "4K", "aac"), Caps{}) {
		t.Error("no caps: 4K HDR should keep needing a Light copy")
	}
	if !playsIn(mp4("H.264", "", "1080p", "aac"), Caps{}) {
		t.Error("no caps: plain 1080p MP4 should play")
	}
}
