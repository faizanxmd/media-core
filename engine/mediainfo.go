package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MInfo is the short picture/sound summary the Library tab shows for a video:
// "4K · Dolby Vision 8.1 · 53 Mb/s · DTS-HD MA 5.1".
type MInfo struct {
	Res      string    `json:"res,omitempty"`    // 4K, 1080p, 720p…
	HDR      string    `json:"hdr,omitempty"`    // Dolby Vision 8.1, HDR10, HLG
	Mbps     float64   `json:"mbps,omitempty"`   // overall bitrate
	Audio    string    `json:"audio,omitempty"`  // default audio track: TrueHD Atmos 7.1
	VCodec   string    `json:"vcodec,omitempty"` // HEVC Main 10
	Tracks   []string  `json:"tracks,omitempty"` // every audio track, with language
	Subs     []string  `json:"subs,omitempty"`   // embedded subtitle languages
	Bits     int       `json:"bits,omitempty"`   // music: bit depth
	Rate     int       `json:"rate,omitempty"`   // music: sample rate (Hz)
	Dur      float64   `json:"dur,omitempty"`    // seconds
	FPS      float64   `json:"fps,omitempty"`    // video frame rate, for the TV's refresh-rate switch
	Width    int       `json:"w,omitempty"`
	Chapters []Chapter `json:"chapters,omitempty"`
	Height   int       `json:"h,omitempty"`
}

const miVersion = 4 // bump when MInfo gains fields, so cached entries are re-read

// ffprobe reads only the file's header, but on the HDD that's still a seek
// per file, so results are cached by size+mtime in mediainfo.json.
type miEntry struct {
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
	Info  *MInfo `json:"info"`
	V     int    `json:"v"`
}

var (
	miMu    sync.Mutex
	miCache map[string]miEntry
	miPath  = filepath.Join(DataDir, "mediainfo.json")
)

func mediaInfo(path string, fi os.FileInfo) *MInfo {
	miMu.Lock()
	if miCache == nil {
		miCache = map[string]miEntry{}
		if b, err := os.ReadFile(miPath); err == nil {
			json.Unmarshal(b, &miCache)
		}
	}
	e, ok := miCache[path]
	miMu.Unlock()
	if ok && e.V == miVersion && e.Size == fi.Size() && e.Mtime == fi.ModTime().Unix() {
		return e.Info
	}
	info := probeMedia(path)
	if info == nil { // unreadable (still being written?): try again next time
		return nil
	}
	miMu.Lock()
	miCache[path] = miEntry{fi.Size(), fi.ModTime().Unix(), info, miVersion}
	miMu.Unlock()
	return info
}

// flushMediaInfo writes the cache without pruning (the catalog only sees videos).
func flushMediaInfo() {
	miMu.Lock()
	defer miMu.Unlock()
	b, _ := json.Marshal(miCache)
	os.WriteFile(miPath+".tmp", b, 0o644)
	os.Rename(miPath+".tmp", miPath)
}

// Chapter is a named chapter from the file (seeking, intro/credits markers).
type Chapter struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Title string  `json:"title,omitempty"`
}

// frameRate turns ffprobe's "24000/1001" into 23.976.
func frameRate(s string) float64 {
	var a, b float64
	if n, _ := fmt.Sscanf(s, "%f/%f", &a, &b); n == 2 && b > 0 {
		return float64(int(a/b*1000+0.5)) / 1000
	}
	return 0
}

// saveMediaInfo drops entries for files that are gone and writes the cache.
func saveMediaInfo(seen map[string]bool) {
	miMu.Lock()
	defer miMu.Unlock()
	for p := range miCache {
		if !seen[p] {
			delete(miCache, p)
		}
	}
	b, _ := json.Marshal(miCache)
	os.WriteFile(miPath+".tmp", b, 0o644)
	os.Rename(miPath+".tmp", miPath)
}

func probeMedia(path string) *MInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries",
		"format=bit_rate,duration:stream=codec_type,codec_name,profile,width,height,color_transfer,channels,avg_frame_rate,r_frame_rate,"+
			"bits_per_raw_sample,bits_per_sample,sample_rate:stream_tags=language,title:"+
			"stream_side_data=dv_profile,dv_bl_signal_compatibility_id:stream_disposition=default,forced,attached_pic",
		"-show_chapters", "-of", "json", path).Output()
	if err != nil {
		return nil
	}
	var r struct {
		Chapters []struct {
			Start string `json:"start_time"`
			End   string `json:"end_time"`
			Tags  struct{ Title string }
		}
		Format struct {
			BitRate  string `json:"bit_rate"`
			Duration string `json:"duration"`
		}
		Streams []struct {
			Type     string `json:"codec_type"`
			Codec    string `json:"codec_name"`
			Profile  string
			Width    int
			Height   int
			Transfer string `json:"color_transfer"`
			AvgFR    string `json:"avg_frame_rate"`
			RFR      string `json:"r_frame_rate"`
			Channels int
			RawBits  string `json:"bits_per_raw_sample"`
			Bits     int    `json:"bits_per_sample"`
			Rate     string `json:"sample_rate"`
			Tags     struct{ Language string }
			Side     []struct {
				DV     int `json:"dv_profile"`
				Compat int `json:"dv_bl_signal_compatibility_id"`
			} `json:"side_data_list"`
			Disp struct {
				Default, Forced int
				Pic             int `json:"attached_pic"`
			} `json:"disposition"`
		}
	}
	if json.Unmarshal(out, &r) != nil {
		return nil
	}
	m := &MInfo{}
	var br float64
	fmt.Sscan(r.Format.BitRate, &br)
	m.Mbps = float64(int(br/1e5+0.5)) / 10
	fmt.Sscan(r.Format.Duration, &m.Dur)
	for _, c := range r.Chapters {
		var ch Chapter
		fmt.Sscan(c.Start, &ch.Start)
		fmt.Sscan(c.End, &ch.End)
		ch.Title = c.Tags.Title
		ch.Start, ch.End = round1(ch.Start), round1(ch.End)
		m.Chapters = append(m.Chapters, ch)
	}
	video, audio := -1, -1
	for i, s := range r.Streams {
		if s.Type == "video" && video < 0 && s.Disp.Pic == 0 && s.Codec != "mjpeg" && s.Codec != "png" { // skip cover art
			video = i
		}
		if s.Type == "audio" && (audio < 0 || s.Disp.Default == 1 && r.Streams[audio].Disp.Default == 0) {
			audio = i
		}
		switch s.Type {
		case "audio":
			t := audioLabel(s.Codec, s.Profile, s.Channels)
			if l := langName(s.Tags.Language); l != "" {
				t += " · " + l
			}
			m.Tracks = append(m.Tracks, t)
		case "subtitle":
			l := langName(s.Tags.Language)
			if l == "" {
				l = "Unknown"
			}
			if s.Disp.Forced == 1 {
				l += " (forced)"
			}
			if !slices.Contains(m.Subs, l) {
				m.Subs = append(m.Subs, l)
			}
		}
	}
	if video >= 0 {
		v := r.Streams[video]
		m.Width, m.Height = v.Width, v.Height
		m.FPS = frameRate(v.AvgFR)
		if m.FPS == 0 || m.FPS > 120 {
			m.FPS = frameRate(v.RFR)
		}
		switch w, h := v.Width, v.Height; {
		case w >= 3800 || h >= 2100:
			m.Res = "4K"
		case w >= 1900 || h >= 1060:
			m.Res = "1080p"
		case w >= 1260 || h >= 700:
			m.Res = "720p"
		case h > 0:
			m.Res = fmt.Sprintf("%dp", h)
		}
		for _, sd := range v.Side {
			if sd.DV > 0 {
				m.HDR = fmt.Sprintf("Dolby Vision %d", sd.DV)
				if sd.Compat > 0 {
					m.HDR += fmt.Sprintf(".%d", sd.Compat)
				}
			}
		}
		m.VCodec = map[string]string{"hevc": "HEVC", "h264": "H.264", "av1": "AV1", "vp9": "VP9", "mpeg4": "MPEG-4", "mpeg2video": "MPEG-2"}[v.Codec]
		if m.VCodec == "" {
			m.VCodec = strings.ToUpper(v.Codec)
		}
		if v.Profile == "Main 10" {
			m.VCodec += " Main 10"
		}
		if m.HDR == "" {
			switch v.Transfer {
			case "smpte2084":
				m.HDR = "HDR10"
			case "arib-std-b67":
				m.HDR = "HLG"
			}
		}
	}
	if audio >= 0 {
		a := r.Streams[audio]
		m.Audio = audioLabel(a.Codec, a.Profile, a.Channels)
		if video < 0 { // music: quality instead of a track list
			m.Tracks = nil
			m.Bits, _ = strconv.Atoi(a.RawBits)
			if m.Bits == 0 {
				m.Bits = a.Bits
			}
			m.Rate, _ = strconv.Atoi(a.Rate)
		}
	}
	return m
}

func audioLabel(codec, profile string, ch int) string {
	name := map[string]string{"truehd": "TrueHD", "eac3": "DD+", "ac3": "Dolby Digital", "dts": "DTS", "aac": "AAC",
		"flac": "FLAC", "opus": "Opus", "mp3": "MP3", "vorbis": "Vorbis", "pcm_s16le": "PCM", "pcm_s24le": "PCM"}[codec]
	if name == "" {
		name = strings.ToUpper(codec)
	}
	switch {
	case strings.Contains(profile, "Atmos"):
		name += " Atmos"
	case codec == "dts" && strings.HasPrefix(profile, "DTS-HD MA"):
		name = "DTS-HD MA"
	case codec == "dts" && strings.Contains(profile, "X"):
		name = "DTS:X"
	}
	switch {
	case ch >= 8:
		name += " 7.1"
	case ch >= 6:
		name += " 5.1"
	case ch == 2:
		name += " stereo"
	case ch == 1:
		name += " mono"
	}
	return name
}

var langs = map[string]string{"en": "English", "eng": "English", "hi": "Hindi", "hin": "Hindi", "ko": "Korean", "kor": "Korean",
	"es": "Spanish", "spa": "Spanish", "fr": "French", "fre": "French", "fra": "French", "de": "German", "ger": "German",
	"deu": "German", "ja": "Japanese", "jpn": "Japanese", "it": "Italian", "ita": "Italian", "pt": "Portuguese", "por": "Portuguese",
	"zh": "Chinese", "chi": "Chinese", "zho": "Chinese", "ar": "Arabic", "ara": "Arabic", "ru": "Russian", "rus": "Russian",
	"ta": "Tamil", "tam": "Tamil", "te": "Telugu", "tel": "Telugu", "ur": "Urdu", "urd": "Urdu", "nl": "Dutch", "dut": "Dutch",
	"nld": "Dutch", "sv": "Swedish", "swe": "Swedish", "da": "Danish", "dan": "Danish", "no": "Norwegian", "nor": "Norwegian",
	"fi": "Finnish", "fin": "Finnish", "pl": "Polish", "pol": "Polish", "tr": "Turkish", "tur": "Turkish", "th": "Thai", "tha": "Thai",
	"ml": "Malayalam", "mal": "Malayalam", "bn": "Bengali", "ben": "Bengali", "pa": "Punjabi", "pan": "Punjabi"}

func langName(code string) string { return langs[strings.ToLower(code)] }
