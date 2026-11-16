package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// What install, setup, upgrade, uninstall and doctor share. Each command's work is a function that returns
// its result or a cliErr, so tests can run it; only the thin *Cmd wrappers print and exit.

// cliErr is a failure with its exit code (and, for the site's settings call, its HTTP status).
type cliErr struct {
	code, status int
	msg          string
}

func (e *cliErr) Error() string { return e.msg }

func cliFail(code int, format string, a ...any) *cliErr {
	status := http.StatusBadRequest
	if code == exitBusy {
		status = http.StatusConflict
	}
	return &cliErr{code: code, status: status, msg: fmt.Sprintf(format, a...)}
}

// The outside world, as variables, so tests never reach the real service manager or the real binary.
var (
	// runSystemctl runs `systemctl --user ...` and returns what it printed.
	runSystemctl = func(args ...string) (string, error) {
		out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	runLoginctl = func(args ...string) (string, error) {
		out, err := exec.Command("loginctl", args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	// journalTail: the last n log lines of one of our units.
	journalTail = func(unit string, n int) string {
		out, _ := exec.Command("journalctl", "--user", "-u", unit, "-n", strconv.Itoa(n), "--no-pager").CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	// hasSystemd: there is a systemd user session to talk to (systemctl alone isn't enough: su and
	// containers have the command but no session).
	hasSystemd = func() bool {
		if _, err := exec.LookPath("systemctl"); err != nil {
			return false
		}
		_, err := runSystemctl("show-environment")
		return err == nil
	}
	// selfExe: the running binary's real path.
	selfExe = func() (string, error) {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		return exe, nil
	}
	// stdinTTY: someone is there to answer questions (/dev/null doesn't count).
	stdinTTY = func() bool {
		fi, err := os.Stdin.Stat()
		if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
			return false
		}
		if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(fi, null) {
			return false
		}
		return true
	}
	helloWait  = 30 * time.Second // install: how long the service gets to answer
	healthWait = 45 * time.Second // upgrade: the same, for the new version
)

// binPath: where install puts the binary. MEDIA_CORE_INSTALL_DIR moves it (install.sh passes it on).
func binPath() string {
	dir := filepath.Join(homeDir(), ".local", "bin")
	if d := os.Getenv("MEDIA_CORE_INSTALL_DIR"); filepath.IsAbs(d) {
		dir = filepath.Clean(d)
	}
	return filepath.Join(dir, "media-core-engine")
}

// cmdName: how to call the binary in a line someone will paste: by name when its folder is on the PATH,
// else by its full path.
func cmdName(bin string) string {
	if bin == "" || onPath(filepath.Dir(bin)) {
		return "media-core-engine"
	}
	return shellQuote(bin)
}

// userName: the account's name, from the system rather than $USER (which su and cron leave wrong or empty).
func userName() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

func onPath(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p != "" && filepath.Clean(p) == dir {
			return true
		}
	}
	return false
}

func expandHome(p string) string {
	if p == "~" {
		return homeDir()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), p[2:])
	}
	return p
}

// ask prints a question on stderr and reads a line; def when nothing is typed.
func ask(in *bufio.Reader, q, def string) string {
	if def != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", q, def)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", q)
	}
	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		return def
	}
	if line = strings.TrimSpace(line); line == "" {
		return def
	}
	return line
}

func askYesIn(in *bufio.Reader, q string) bool {
	a := strings.ToLower(ask(in, q+" (y/n)", "n"))
	return a == "y" || a == "yes"
}

// sayTo: where a command's words for people go. With --json they move to stderr, so stdout holds one object.
func sayTo(asJSON bool) func(string, ...any) {
	w := io.Writer(os.Stdout)
	if asJSON {
		w = os.Stderr
	}
	return func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }
}

// cliGet asks the running server with the command line's own key and returns the status and body as they are.
func cliGet(url string, timeout time.Duration) (int, []byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, nil, err
	}
	if b, err := os.ReadFile(cliTokenPath()); err == nil {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(b)))
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, b, err
}

// serverBusy asks the running server whether something is downloading or playing. why finishes the sentence
// "wait until ..." and is "" when nothing is; up is false when no server answers. A server from before
// GET /api/busy answers 404: then the jobs and the system page are read instead. When the server answers
// but can't be understood, that counts as busy: a restart must never be a guess.
var serverBusy = func() (why string, up bool) {
	base := adminURL()
	unknown := func(status int) (string, bool) {
		return fmt.Sprintf("the server can say what it is doing (it answered %d; is %s readable?)", status, cliTokenPath()), true
	}
	status, body, err := cliGet(base+"/api/busy", 5*time.Second)
	if err != nil {
		return "", false
	}
	switch status {
	case http.StatusOK:
		var b struct {
			Busy bool   `json:"busy"`
			Why  string `json:"why"`
		}
		if json.Unmarshal(body, &b) != nil {
			return unknown(status)
		}
		if b.Busy && b.Why == "" {
			b.Why = "the server is idle"
		}
		return b.Why, true
	case http.StatusNotFound:
		status, body, err = cliGet(base+"/api/jobs", 10*time.Second)
		var st struct {
			Jobs []struct {
				State string `json:"state"`
			} `json:"jobs"`
		}
		if err != nil || status != http.StatusOK || json.Unmarshal(body, &st) != nil {
			return unknown(status)
		}
		for _, j := range st.Jobs {
			switch j.State {
			case Queued, Downloading, Paused, Processing, Converting:
				return "the downloads finish", true
			}
		}
		status, body, err = cliGet(base+"/api/system", 10*time.Second)
		var sys struct {
			Playing int `json:"playing"`
		}
		if err != nil || status != http.StatusOK || json.Unmarshal(body, &sys) != nil {
			return unknown(status)
		}
		if sys.Playing > 0 {
			return "nothing is playing", true
		}
		return "", true
	}
	return unknown(status)
}

// helloVersion: the running server's version ("" when it doesn't answer).
func helloVersion() (string, bool) {
	c := &http.Client{Timeout: 2 * time.Second}
	r, err := c.Get("http://127.0.0.1:" + portOf(playerAddr(), "8790") + "/api/v1/hello")
	if err != nil {
		return "", false
	}
	defer r.Body.Close()
	var h struct {
		Version string `json:"version"`
	}
	json.NewDecoder(r.Body).Decode(&h)
	return h.Version, r.StatusCode == http.StatusOK
}

// waitHello waits until the running server answers (with version want, unless want is "").
func waitHello(d time.Duration, want string) bool {
	for end := time.Now().Add(d); ; time.Sleep(300 * time.Millisecond) {
		if v, ok := helloVersion(); ok && (want == "" || v == want) {
			return true
		}
		if !time.Now().Before(end) {
			return false
		}
	}
}

// siteURLs: the site's address on each home-network address of this computer.
func siteURLs() []string {
	var out []string
	lan, _ := helloInfo()["lan"].([]string)
	for _, u := range lan {
		out = append(out, strings.TrimSuffix(u, ":"+portOf(playerAddr(), "8790"))+":"+sitePort()+"/")
	}
	if len(out) == 0 {
		out = []string{"http://localhost:" + sitePort() + "/"}
	}
	return out
}

// copyExecutable copies a binary into place through a temp file, so a running one is replaced, not written into.
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp")
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if e := out.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Chmod(tmp, 0o755)
	}
	if err == nil {
		err = renameFile(tmp, dst)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// renameFile is os.Rename; a test records the order files land in.
var renameFile = os.Rename
