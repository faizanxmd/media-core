package main

import (
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	Listen = ":8780" // the site and Manager
	// defaultUA is what downloads and link checks say they are: many file hosts refuse an unknown agent,
	// so it's a common browser's. MEDIA_CORE_USER_AGENT replaces it.
	defaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"
	// MB/s used for ETAs until a real download has been measured.
	DefaultMBps = 3.5
)

var (
	ConfDir   = defaultConfDir()
	DataDir   = defaultDataDir()
	JobsDir   = filepath.Join(DataDir, "jobs")
	Aria2Conf = filepath.Join(ConfDir, "aria2.conf")
	// Media is the library root: MEDIA_CORE_LIBRARY (install writes it to engine.env), else ~/Media.
	Media    = envOr("MEDIA_CORE_LIBRARY", filepath.Join(homeDir(), "Media"))
	DoviTool = envOr("MEDIA_CORE_DOVI_TOOL", defaultDoviTool())
	UA       = envOr("MEDIA_CORE_USER_AGENT", defaultUA)

	VideoExt   = set("mkv", "mp4", "m4v", "avi", "mov", "ts", "m2ts", "webm")
	AudioExt   = set("flac", "mp3", "m4a", "aac", "ogg", "opus", "wav", "alac", "aiff")
	ArchiveExt = set("zip", "rar", "7z")
	SubExt     = set("srt", "ass", "ssa", "sub", "idx", "vtt")
)

var (
	envOnce sync.Once
	envFile map[string]string
)

// The folders are fixed under the home folder (no XDG_* lookup): the aria2 unit, install.sh and the
// backup script use the same paths, so the engine must not wander off on its own.
// setPaths points every state path at conf, data and the library. main calls it before anything is opened
// (serve --library), so the package-level defaults above are only defaults.
func setPaths(conf, data, media string) {
	ConfDir, DataDir, Media = conf, data, media
	// settings come from this ConfDir's engine.env from now on (a temporary server never reads the installed one's)
	envOnce, envFile = sync.Once{}, nil
	DoviTool = envOr("MEDIA_CORE_DOVI_TOOL", defaultDoviTool())
	JobsDir = filepath.Join(DataDir, "jobs")
	Aria2Conf = filepath.Join(ConfDir, "aria2.conf")
	introsPath = filepath.Join(DataDir, "intros.json")
	miPath = filepath.Join(DataDir, "mediainfo.json")
	tmdbDir = filepath.Join(DataDir, "tmdb")
	settingsPath = filepath.Join(DataDir, "settings.json")
	watchPath = filepath.Join(DataDir, "watch.json")
}

func defaultConfDir() string {
	if v := trimSlash(os.Getenv("MEDIA_CORE_CONFIG_DIR")); v != "" {
		return v
	}
	return filepath.Join(homeDir(), ".config/media-core")
}

func defaultDataDir() string {
	if v := trimSlash(os.Getenv("MEDIA_CORE_DATA_DIR")); v != "" {
		return v
	}
	return filepath.Join(homeDir(), ".local/share/media-core")
}

// homeDir is $HOME, else the account's home folder, so paths never end up relative to where it started.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return "/"
}

// trimSlash drops trailing slashes but keeps "/" itself.
func trimSlash(v string) string {
	if t := strings.TrimRight(v, "/"); t != "" || v == "" {
		return t
	}
	return "/"
}

func defaultDoviTool() string {
	if p, err := exec.LookPath("dovi_tool"); err == nil && p != "" {
		return p
	}
	return filepath.Join(homeDir(), ".local/bin/dovi_tool")
}

func parseEnvFile(path string) map[string]string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	m := map[string]string{}
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		m[k] = v
	}
	return m
}

func loadEnv() map[string]string {
	envOnce.Do(func() {
		if testing.Testing() {
			return
		}
		envFile = parseEnvFile(filepath.Join(ConfDir, "engine.env"))
	})
	return envFile
}

func envOrWith(key, def string, file map[string]string) string {
	if v := os.Getenv(key); v != "" {
		return trimSlash(v)
	}
	if v := file[key]; v != "" {
		return trimSlash(v)
	}
	return def
}

func envOrFrom(key, def, path string) string { return envOrWith(key, def, parseEnvFile(path)) }

func envOr(key, def string) string { return envOrWith(key, def, loadEnv()) }

func listenAddr() string { return envOr("MEDIA_CORE_LISTEN", Listen) }

func playerAddr() string { return envOr("MEDIA_CORE_PLAYER_LISTEN", PlayerListen) }

func portOf(addr, def string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil && port != "" {
		return port
	}
	return def
}

func serverName() string {
	if v := envOr("MEDIA_CORE_NAME", ""); v != "" {
		return v
	}
	if h, err := os.Hostname(); err == nil {
		if i := strings.IndexByte(h, '.'); i >= 0 {
			h = h[:i]
		}
		if h != "" {
			return h
		}
	}
	return "Media Core"
}

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// ext returns the lower-case extension without the dot.
func ext(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

// rel shows a library path the way the UI does: relative to the library root.
func rel(p string) string { return strings.TrimPrefix(p, Media+"/") }
