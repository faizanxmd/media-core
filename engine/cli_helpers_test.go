package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHome gives a test its own home, config, data and library folders, and puts the real ones back after.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	oldConf, oldData, oldMedia := ConfDir, DataDir, Media
	t.Setenv("HOME", home)
	t.Setenv("MEDIA_CORE_INSTALL_DIR", "")
	t.Setenv("MEDIA_CORE_TMDB_KEY", "")
	t.Setenv("MEDIA_CORE_LIBRARY", "")
	setPaths(filepath.Join(home, ".config/media-core"), filepath.Join(home, ".local/share/media-core"), filepath.Join(home, "Media"))
	t.Cleanup(func() { setPaths(oldConf, oldData, oldMedia) })
	return home
}

// fakeSystem stands in for systemd, loginctl, the journal, the terminal and the running binary. calls holds
// every systemctl and loginctl command line.
type fakeSystem struct {
	mu     sync.Mutex
	calls  []string
	fail   map[string]bool   // a command line (or its first word) that fails
	answer map[string]string // a command line's output
	busy   string
}

func (f *fakeSystem) run(prefix string, args ...string) (string, error) {
	line := strings.TrimSpace(prefix + " " + strings.Join(args, " "))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, line)
	if f.fail[line] || f.fail[strings.Fields(line)[0]] {
		return "", fmt.Errorf("exit status 1")
	}
	return f.answer[line], nil
}

func (f *fakeSystem) called(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func stubSystem(t *testing.T, exe string) *fakeSystem {
	t.Helper()
	f := &fakeSystem{fail: map[string]bool{}, answer: map[string]string{}}
	oldS, oldL, oldJ, oldH, oldE, oldT, oldB, oldP, oldSI, oldHW, oldHe := runSystemctl, runLoginctl, journalTail, hasSystemd, selfExe, stdinTTY,
		serverBusy, portTaken, serviceInstalled, helloWait, healthWait
	t.Cleanup(func() {
		runSystemctl, runLoginctl, journalTail, hasSystemd, selfExe, stdinTTY, serverBusy, portTaken, serviceInstalled, helloWait, healthWait =
			oldS, oldL, oldJ, oldH, oldE, oldT, oldB, oldP, oldSI, oldHW, oldHe
	})
	runSystemctl = func(args ...string) (string, error) { return f.run("", args...) }
	runLoginctl = func(args ...string) (string, error) { return f.run("loginctl", args...) }
	journalTail = func(unit string, n int) string { return "(journal of " + unit + ")" }
	hasSystemd = func() bool { return true }
	selfExe = func() (string, error) { return exe, nil }
	stdinTTY = func() bool { return false }
	serverBusy = func() (string, bool) { return f.busy, true }
	portTaken = func(int) bool { return false }
	serviceInstalled = func() bool { return true }
	helloWait, healthWait = 2*time.Second, 2*time.Second
	return f
}

// helloServer plays the running server's apps port: it answers hello with whatever version() says.
func helloServer(t *testing.T, version func() string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/hello" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"version": version(), "name": "test"})
	}))
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	t.Setenv("MEDIA_CORE_PLAYER_LISTEN", ":"+port)
}

// noServer points the apps port where nothing listens.
func noServer(t *testing.T) {
	t.Helper()
	p, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIA_CORE_PLAYER_LISTEN", fmt.Sprintf(":%d", p))
	t.Setenv("MEDIA_CORE_LISTEN", fmt.Sprintf(":%d", p))
}

// releaseHost plays the release host: <base>/latest and <base>/download/<tag>/<asset>. sums names the
// checksum file it writes ("SHA256SUMS", or "APPS.SHA256SUMS" for an older release); wrongSum makes one
// asset's line a lie.
type releaseHost struct {
	mu       sync.Mutex
	requests []string
}

func (h *releaseHost) asked(part string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.requests {
		if strings.Contains(r, part) {
			return true
		}
	}
	return false
}

func fakeReleaseHost(t *testing.T, tag string, assets map[string]string, sums, wrongSum string) *releaseHost {
	t.Helper()
	var list strings.Builder
	for name, body := range assets {
		sum := sha256.Sum256([]byte(body))
		if name == wrongSum {
			sum = sha256.Sum256([]byte("something else"))
		}
		fmt.Fprintf(&list, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	h := &releaseHost{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.requests = append(h.requests, r.URL.Path)
		h.mu.Unlock()
		name := strings.TrimPrefix(r.URL.Path, "/download/"+tag+"/")
		switch {
		case r.URL.Path == "/latest":
			fmt.Fprintln(w, tag)
		case name == sums:
			w.Write([]byte(list.String()))
		case assets[name] != "" && name != r.URL.Path:
			w.Write([]byte(assets[name]))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MEDIA_CORE_BASE_URL", srv.URL)
	return h
}

func writeFile(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// treeHash: every file under dir with its contents' hash, to show a folder wasn't touched.
func treeHash(t *testing.T, dir string) string {
	t.Helper()
	var out strings.Builder
	filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if fi.IsDir() {
			fmt.Fprintf(&out, "%s/\n", p)
			return nil
		}
		b, _ := os.ReadFile(p)
		sum := sha256.Sum256(b)
		fmt.Fprintf(&out, "%s %x %v\n", p, sum, fi.Mode())
		return nil
	})
	return out.String()
}

func say(t *testing.T) func(string, ...any) {
	return func(format string, a ...any) { t.Logf(format, a...) }
}
