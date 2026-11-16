package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeFFprobe(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "ffprobe")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestMediaInfoHDR10PlusAndTrackTitle(t *testing.T) {
	// Emulate ffprobe's -show_entries behavior: side_data_type is only emitted if requested.
	writeFakeFFprobe(t, `
args="$*"
has_sdt=""
case "$args" in
  *side_data_type*) has_sdt='"side_data_type": "HDR Dynamic Metadata SMPTE2094-40"' ;;
esac
cat <<EOF
{
  "format": {"bit_rate": "25000000", "duration": "120.0", "format_name": "matroska,webm"},
  "streams": [
    {
      "codec_type": "video",
      "codec_name": "hevc",
      "profile": "Main 10",
      "width": 3840,
      "height": 2160,
      "color_transfer": "smpte2084",
      "avg_frame_rate": "24000/1001",
      "side_data_list": [{$has_sdt}]
    },
    {
      "codec_type": "audio",
      "codec_name": "eac3",
      "channels": 6,
      "tags": {"language": "eng", "title": "Director's Commentary"},
      "disposition": {"default": 1}
    }
  ]
}
EOF
`)

	m := probeMedia("/fake/movie.mkv")
	if m == nil {
		t.Fatal("probeMedia returned nil")
	}
	if m.HDR != "HDR10+" {
		t.Errorf("HDR = %q, want %q", m.HDR, "HDR10+")
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Director's Commentary") {
		t.Errorf("track title missing from MInfo JSON: %s", b)
	}
}

func TestMediaInfoCacheReprobesOldVersion(t *testing.T) {
	oldPath := miPath
	miPath = filepath.Join(t.TempDir(), "mediainfo.json")
	defer func() {
		miMu.Lock()
		miCache = nil
		miMu.Unlock()
		miPath = oldPath
	}()

	f := filepath.Join(t.TempDir(), "film.mkv")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}

	// Seed a v6 cache entry with HDR10.
	miMu.Lock()
	miCache = map[string]miEntry{
		f: {Size: fi.Size(), Mtime: fi.ModTime().Unix(), V: 6, Info: &MInfo{HDR: "HDR10"}},
	}
	miMu.Unlock()

	writeFakeFFprobe(t, `cat <<'EOF'
{
  "format": {"bit_rate": "10000000", "duration": "60.0", "format_name": "matroska,webm"},
  "streams": [
    {
      "codec_type": "video",
      "codec_name": "hevc",
      "profile": "Main 10",
      "width": 3840,
      "height": 2160,
      "color_transfer": "smpte2084",
      "side_data_list": [{"side_data_type": "HDR Dynamic Metadata SMPTE2094-40"}]
    }
  ]
}
EOF
`)
	got := mediaInfo(f, fi)
	if got == nil || got.HDR != "HDR10+" {
		t.Fatalf("expected v6 entry to re-probe as HDR10+, got %+v", got)
	}
}
