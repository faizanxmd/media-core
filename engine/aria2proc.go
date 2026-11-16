package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The downloader (aria2) runs as its own systemd user unit on an installed server, so downloads survive an
// engine restart. Without that unit (a temporary server, a Mac) the engine runs aria2 itself and stops it on exit.

// aria2ConfText is the downloader's settings. dataDir holds its session file; alloc is "falloc" where the
// library's filesystem supports it cheaply, else "none".
func aria2ConfText(secret, dataDir, alloc string, port int) string {
	session := filepath.Join(dataDir, "aria2.session")
	return strings.Join([]string{
		"# aria2 for Media Core. RPC on localhost only.",
		"enable-rpc=true", "rpc-listen-port=" + strconv.Itoa(port), "rpc-listen-all=false", "rpc-secret=" + secret,
		"input-file=" + session, "save-session=" + session, "save-session-interval=20",
		"continue=true", "auto-file-renaming=false", "allow-overwrite=false",
		"max-concurrent-downloads=1", "max-download-result=200",
		"max-connection-per-server=16", "split=16", "min-split-size=8M",
		"max-tries=20", "retry-wait=15", "connect-timeout=30", "timeout=60",
		"user-agent=" + UA,
		"file-allocation=" + alloc, "disk-cache=32M",
		"summary-interval=0", "console-log-level=warn", "",
	}, "\n")
}

// ensureAria2Conf writes the downloader's settings (with a fresh random secret, RPC on port) and its session
// file if they are missing. It never changes an existing file.
func ensureAria2Conf(port int) error {
	if err := os.MkdirAll(DataDir, 0o755); err != nil {
		return err
	}
	if f, err := os.OpenFile(filepath.Join(DataDir, "aria2.session"), os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.Close()
	}
	if _, err := os.Stat(Aria2Conf); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(Aria2Conf), 0o700); err != nil {
		return err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	return os.WriteFile(Aria2Conf, []byte(aria2ConfText(hex.EncodeToString(b), DataDir, fileAlloc(Media), port)), 0o600)
}

var (
	ariaChildMu   sync.Mutex
	ariaChildProc *os.Process
	ariaChildDone chan struct{} // closed when the child has exited
	ariaStopping  bool          // the engine is stopping: no new child
	// ariaPort, when set, is the RPC port a child aria2 is told on its command line (a temporary server picks a
	// free one each start); NewAria2 uses it too. 0: whatever aria2.conf says.
	ariaPort int
)

// startAria2 brings the downloader up: its systemd unit when there is one, else a child process.
// It's called when the RPC doesn't answer, at most every 30 s. A unit that exists but won't start is left
// to systemd: a second aria2 next to it would fight it for the port.
var startAria2 = func() {
	err := exec.Command("systemctl", "--user", "start", "media-core-aria2.service").Run()
	if err == nil {
		return
	}
	var ee *exec.ExitError
	noUnit := errors.Is(err, exec.ErrNotFound) || (errors.As(err, &ee) && ee.ExitCode() == 5)
	if !noUnit {
		log.Printf("aria2: its service won't start (%v); see: systemctl --user status media-core-aria2", err)
		return
	}
	startAria2Child()
}

func childRunning() bool {
	if ariaChildDone == nil {
		return false
	}
	select {
	case <-ariaChildDone:
		return false
	default:
		return true
	}
}

func startAria2Child() {
	ariaChildMu.Lock()
	defer ariaChildMu.Unlock()
	if ariaStopping || childRunning() {
		return // stopping, or still running (or just started): give it time
	}
	bin, err := exec.LookPath("aria2c")
	if err != nil {
		log.Printf("aria2: not installed, so downloads are off (%s)", installHint("aria2c"))
		return
	}
	if err := ensureAria2Conf(6800); err != nil {
		log.Printf("aria2: %v", err)
		return
	}
	// --stop-with-process: aria2 ends with the engine even if the engine is killed or crashes
	args := []string{"--conf-path=" + Aria2Conf, "--stop-with-process=" + strconv.Itoa(os.Getpid())}
	if ariaPort > 0 {
		args = append(args, "--rpc-listen-port="+strconv.Itoa(ariaPort))
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = DataDir
	if err := cmd.Start(); err != nil {
		log.Printf("aria2: %v", err)
		return
	}
	done := make(chan struct{})
	ariaChildProc, ariaChildDone = cmd.Process, done
	log.Printf("aria2: started (pid %d)", cmd.Process.Pid)
	go func() { cmd.Wait(); close(done) }()
}

// stopAria2Child ends a downloader the engine started itself (a unit's aria2 is left alone) and waits up to
// [wait] for it to exit (it saves its session first), killing it after that. No new child starts afterwards.
func stopAria2Child(wait time.Duration) {
	ariaChildMu.Lock()
	ariaStopping = true
	p, done := ariaChildProc, ariaChildDone
	ariaChildMu.Unlock()
	if p == nil {
		return
	}
	p.Signal(os.Interrupt) // aria2 saves its session on SIGINT
	select {
	case <-done:
	case <-time.After(wait):
		p.Kill()
		<-done
	}
}

func describeAria2(err error) string { return fmt.Sprintf("aria2 unreachable (%v)", err) }

// freePort asks the system for a free localhost TCP port (a temporary server's aria2 must not meet the
// installed server's on 6800).
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
