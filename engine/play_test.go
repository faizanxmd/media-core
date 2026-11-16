package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestWatchedWithShowAndOfflineProgress(t *testing.T) {
	sh := &Item{ID: "s1", Type: "show", Title: "Raw Show", Meta: &TMeta{Title: "Real Show", Logo: "/logo.png"}}
	ep := &Item{ID: "e1", Type: "episode", Title: "Ep 1", ShowID: "s1", Show: "Raw Show", Season: 1, Episode: 1, file: "/lib/ep1.mkv"}
	cat := &Catalog{items: map[string]*Item{"s1": sh, "e1": ep}}
	p := &Player{c: cat, w: newTestWatch(), t: &TMDB{}}
	mux := http.NewServeMux()
	p.mount(mux)

	req := httptest.NewRequest("POST", "/api/v1/watched", bytes.NewBufferString(`{"id":"e1","watched":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("watched status = %d", rec.Code)
	}
	var wout struct {
		Item Item `json:"item"`
	}
	json.Unmarshal(rec.Body.Bytes(), &wout)
	if wout.Item.Show != "Real Show" || wout.Item.Meta == nil || wout.Item.Meta.Logo != "/logo.png" {
		t.Fatalf("watched item missing withShow data: show=%q meta=%+v", wout.Item.Show, wout.Item.Meta)
	}

	playMu.Lock()
	playing = map[string]time.Time{}
	playMu.Unlock()

	req2 := httptest.NewRequest("POST", "/api/v1/progress", bytes.NewBufferString(`{"id":"e1","pos":60000,"dur":600000,"at":1700000000}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.RemoteAddr = "192.168.1.50:1234"
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("progress status = %d", rec2.Code)
	}
	if n := playingNow(); n != 0 {
		t.Fatalf("offline progress report touched playing: playingNow()=%d", n)
	}
}

func TestSpeedTestStopsWhenContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "/api/v1/speedtest?bytes=52428800", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	speedTest(rec, req)
	if rec.Body.Len() > 0 {
		t.Fatalf("speedTest wrote %d bytes after context cancelled", rec.Body.Len())
	}
}

func TestServeAPKSendsNoStore(t *testing.T) {
	oldD := DataDir
	DataDir = t.TempDir()
	defer func() { DataDir = oldD }()
	os.MkdirAll(filepath.Join(DataDir, "app"), 0o755)
	os.WriteFile(filepath.Join(DataDir, "app", "media-core.apk"), []byte("apk"), 0o644)

	rec := httptest.NewRecorder()
	serveAPK("media-core")(rec, httptest.NewRequest("GET", "/api/v1/app.apk", nil))
	if rec.Code != 200 {
		t.Fatalf("serveAPK status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestHelloMagicDNSFromTailscale(t *testing.T) {
	magicDNSMu.Lock()
	magicDNSName = ""
	magicDNSMu.Unlock()
	defer func() {
		magicDNSMu.Lock()
		magicDNSName = ""
		magicDNSMu.Unlock()
	}()

	dir := t.TempDir()
	bin := filepath.Join(dir, "tailscale")
	os.WriteFile(bin, []byte("#!/bin/sh\necho '{\"Self\":{\"DNSName\":\"custom-host.tailnet.ts.net.\"}}'\n"), 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	p := &Player{c: &Catalog{items: map[string]*Item{}}, w: newTestWatch(), t: &TMDB{}}
	rec := httptest.NewRecorder()
	p.hello(rec, httptest.NewRequest("GET", "/api/v1/hello", nil))
	var out struct {
		MagicDNS string `json:"magicdns"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.MagicDNS != "http://custom-host.tailnet.ts.net:8790" {
		t.Fatalf("magicdns = %q, want http://custom-host.tailnet.ts.net:8790", out.MagicDNS)
	}
}
