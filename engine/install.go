package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"
)

// install, setup, upgrade and uninstall: everything after the binary is on the computer. The one-line
// install (install.sh) downloads the binary and runs `install`; an AI agent can run the same commands with
// --yes and flags instead of answers. None of them ever touches the library's files.
//
// Linux: systemd user services (aria2, the engine, a nightly backup). macOS: a LaunchAgent for the engine,
// which then runs aria2 itself.

const defaultRepo = "faizanxmd/media-core"

func binPath() string { return filepath.Join(homeDir(), ".local", "bin", "media-core-engine") }

func unitDir() string { return filepath.Join(homeDir(), ".config", "systemd", "user") }

var unitNames = []string{"media-core-aria2.service", "media-core-engine.service", "media-core-backup.service", "media-core-backup.timer"}

type unitVars struct{ Bin, Aria2c, Conf, Data, Path string }

var unitTemplates = map[string]string{
	"media-core-aria2.service": `[Unit]
Description=Media Core downloader (aria2, RPC on localhost only)

[Service]
ExecStart={{.Aria2c}} --conf-path={{.Conf}}/aria2.conf
Restart=on-failure
RestartSec=5
# downloads are network-bound; keep them out of playback's way on CPU and disk
Nice=5
IOSchedulingClass=best-effort
IOSchedulingPriority=6
UMask=0002

[Install]
WantedBy=default.target
`,
	"media-core-engine.service": `[Unit]
Description=Media Core engine: downloads into the library and serves the site and the apps
After=network-online.target media-core-aria2.service
Wants=media-core-aria2.service

[Service]
ExecStart={{.Bin}} serve
Restart=always
RestartSec=3
Environment=PATH={{.Path}}
UMask=0002
# post-processing (ffmpeg, 7z) picks up again after a restart, so a quick stop is fine
TimeoutStopSec=10
# a stream keeps up to about 150M of pieces in memory while a title plays (stream.go)
MemoryHigh=800M

[Install]
WantedBy=default.target
`,
	"media-core-backup.service": `[Unit]
Description=Media Core: back up watch progress, settings and devices

[Service]
Type=oneshot
ExecStart={{.Bin}} backup
Nice=19
IOSchedulingClass=idle
`,
	"media-core-backup.timer": `[Unit]
Description=Media Core: nightly backup

[Timer]
OnCalendar=*-*-* 04:00:00
Persistent=true

[Install]
WantedBy=timers.target
`,
}

const launchAgent = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>app.mediacore.engine</string>
  <key>ProgramArguments</key><array><string>{{.Bin}}</string><string>serve</string></array>
  <key>EnvironmentVariables</key><dict><key>PATH</key><string>{{.Path}}</string></dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardErrorPath</key><string>{{.Data}}/engine.log</string>
  <key>StandardOutPath</key><string>{{.Data}}/engine.log</string>
</dict>
</plist>
`

func launchAgentPath() string {
	return filepath.Join(homeDir(), "Library", "LaunchAgents", "app.mediacore.engine.plist")
}

func render(tmpl string, v unitVars) string {
	var b bytes.Buffer
	template.Must(template.New("").Parse(tmpl)).Execute(&b, v)
	return b.String()
}

// writeIfChanged writes text to path when it differs, and says whether it did.
func writeIfChanged(path, text string, mode os.FileMode) (bool, error) {
	if b, err := os.ReadFile(path); err == nil && string(b) == text {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path+".tmp", []byte(text), mode); err != nil {
		return false, err
	}
	return true, os.Rename(path+".tmp", path)
}

func currentUnitVars(bin string) unitVars {
	aria, _ := exec.LookPath("aria2c")
	if aria == "" {
		aria = "/usr/bin/aria2c"
	}
	path := filepath.Join(homeDir(), ".local", "bin") + ":/usr/local/bin:/usr/bin:/bin"
	if runtime.GOOS == "darwin" {
		path = filepath.Join(homeDir(), ".local", "bin") + ":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
	}
	return unitVars{Bin: bin, Aria2c: aria, Conf: ConfDir, Data: DataDir, Path: path}
}

// ask prints a question and reads a line; def when nothing is typed (or there's no one to ask).
func ask(in *bufio.Reader, q, def string) string {
	if in == nil {
		return def
	}
	if def != "" {
		fmt.Printf("%s [%s]: ", q, def)
	} else {
		fmt.Printf("%s: ", q)
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

func expandHome(p string) string {
	if p == "~" {
		return homeDir()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), p[2:])
	}
	return p
}

type installOpts struct {
	library, name, tmdbKey string
	yes, quiet             bool
}

func installFlags(name string, args []string) installOpts {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	var o installOpts
	fs.StringVar(&o.library, "library", "", "the library folder (Movies, TV Shows and Music go in it)")
	fs.StringVar(&o.name, "name", "", "the server's name, as the apps show it")
	fs.StringVar(&o.tmdbKey, "tmdb-key", "", "a TMDB API key, for posters and details")
	fs.BoolVar(&o.yes, "yes", false, "ask nothing; use the flags and defaults")
	fs.Parse(args)
	return o
}

// installCmd sets this computer up as a server and starts it. Safe to run again: it keeps every setting
// and secret, rewrites the service files only if they changed, and restarts the engine.
func installCmd(o installOpts) {
	var in *bufio.Reader
	if !o.yes {
		if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			in = bufio.NewReader(os.Stdin)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		fail(err.Error())
	}
	exe, _ = filepath.EvalSymlinks(exe)
	if err := os.MkdirAll(ConfDir, 0o700); err != nil {
		fail(err.Error())
	}
	os.Chmod(ConfDir, 0o700)
	if err := os.MkdirAll(DataDir, 0o755); err != nil {
		fail(err.Error())
	}
	envPath := filepath.Join(ConfDir, "engine.env")

	// the library: a flag, else what's set already, else ask (the default is ~/Media)
	lib := expandHome(o.library)
	if lib == "" {
		if set := envOr("MEDIA_CORE_LIBRARY", ""); set != "" {
			lib = set
		} else {
			lib = expandHome(ask(in, "Library folder (your films, shows and music go here)", filepath.Join(homeDir(), "Media")))
		}
	}
	if !filepath.IsAbs(lib) {
		fail("the library folder needs a full path, like /srv/media or ~/Media")
	}
	lib = filepath.Clean(lib)
	for _, f := range libraryFolders {
		if err := os.MkdirAll(filepath.Join(lib, f), 0o775); err != nil {
			fail("couldn't make " + filepath.Join(lib, f) + ": " + err.Error())
		}
	}
	if p := libraryProblem(lib); p != "" {
		fail(p)
	}
	if envOrFrom("MEDIA_CORE_LIBRARY", "", envPath) != lib {
		if err := setEnvLine(envPath, "MEDIA_CORE_LIBRARY", lib); err != nil {
			fail(err.Error())
		}
	}
	if o.name != "" {
		if err := setEnvLine(envPath, "MEDIA_CORE_NAME", cleanName(o.name, "")); err != nil {
			fail(err.Error())
		}
	}
	if o.tmdbKey != "" {
		if err := checkTMDBKey(o.tmdbKey); err != nil {
			fail(err.Error())
		}
		if err := os.WriteFile(filepath.Join(ConfDir, "tmdb.key"), []byte(o.tmdbKey+"\n"), 0o600); err != nil {
			fail(err.Error())
		}
	}
	setPaths(ConfDir, DataDir, lib)
	if err := ensureAria2Conf(6800); err != nil {
		fail(err.Error())
	}

	v := currentUnitVars(exe)
	managed := true
	switch {
	case runtime.GOOS == "linux" && hasSystemd():
		for _, n := range unitNames {
			changed, err := writeIfChanged(filepath.Join(unitDir(), n), render(unitTemplates[n], v), 0o644)
			if err != nil {
				fail(err.Error())
			}
			if changed {
				fmt.Println("wrote", filepath.Join(unitDir(), n))
			}
		}
		run("systemctl", "--user", "daemon-reload")
		run("systemctl", "--user", "enable", "--now", "media-core-aria2.service", "media-core-engine.service", "media-core-backup.timer")
		run("systemctl", "--user", "restart", "media-core-engine.service")
	case runtime.GOOS == "darwin":
		if _, err := writeIfChanged(launchAgentPath(), render(launchAgent, v), 0o644); err != nil {
			fail(err.Error())
		}
		exec.Command("launchctl", "unload", launchAgentPath()).Run()
		run("launchctl", "load", "-w", launchAgentPath())
	default:
		managed = false
	}

	// what needs the owner (sudo): missing tools, and staying up after logout
	var lines []string
	for _, t := range toolStates() {
		if !t.Found && t.Install != "" {
			lines = append(lines, t.Install)
		}
	}
	if runtime.GOOS == "linux" && hasSystemd() {
		if u := os.Getenv("USER"); u != "" {
			if out, _ := exec.Command("loginctl", "show-user", u, "-p", "Linger", "--value").Output(); strings.TrimSpace(string(out)) != "yes" {
				lines = append(lines, "sudo loginctl enable-linger "+u+"   # keeps the server running after you log out")
			}
		}
	}
	if len(lines) > 0 {
		fmt.Println("\nStill to do (these need you, and some need sudo):")
		for _, l := range uniq(lines) {
			fmt.Println("  " + l)
		}
	}

	if !managed {
		fmt.Printf("\nSettings are saved. There's no systemd here to run it as a service: start it with\n  %s serve\nthen run media-core-engine setup-code and open %s/#/setup\n", exe, siteURLs())
		return
	}
	if !waitHello(20*time.Second, "") {
		fmt.Println("\nThe server didn't answer yet. Check: media-core-engine doctor")
		os.Exit(2)
	}
	site := siteURLs()
	if d, err := loadDevices(filepath.Join(ConfDir, "devices.json")); err == nil && !d.claimed() {
		code, err := newSetupCode(filepath.Join(ConfDir, "setup.code"))
		if err == nil {
			fmt.Printf("\nMedia Core is running. To finish, open this on a computer or phone at home:\n\n  %s/#/setup\n\nand enter the setup code %s (it lasts 24 hours; media-core-engine setup-code makes a new one).\n", site, code)
		}
	} else {
		fmt.Printf("\nMedia Core is running: %s\n", site)
	}
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fail(name + " " + strings.Join(args, " ") + ": " + err.Error())
	}
}

// siteURLs: the site's address on the home network (the first one), for the setup link.
func siteURLs() string {
	if lan, _ := helloInfo()["lan"].([]string); len(lan) > 0 {
		u := strings.TrimSuffix(lan[0], ":"+portOf(playerAddr(), "8790"))
		return u + ":" + sitePort()
	}
	return "http://localhost:" + sitePort()
}

// waitHello waits until the running server answers (with version want, unless want is "").
func waitHello(d time.Duration, want string) bool {
	u := "http://127.0.0.1:" + portOf(playerAddr(), "8790") + "/api/v1/hello"
	c := &http.Client{Timeout: 2 * time.Second}
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(500 * time.Millisecond) {
		r, err := c.Get(u)
		if err != nil {
			continue
		}
		var h struct{ Version string }
		json.NewDecoder(r.Body).Decode(&h)
		r.Body.Close()
		if want == "" || h.Version == want {
			return true
		}
	}
	return false
}

// setupCmd changes the library, name or TMDB key: through the running server when it answers (it checks
// nothing is playing or downloading, and restarts itself), else straight into the files.
func setupCmd(o installOpts) {
	body := map[string]any{"make_folders": true}
	if o.library != "" {
		body["library"] = expandHome(o.library)
	}
	if o.name != "" {
		body["name"] = o.name
	}
	if o.tmdbKey != "" {
		body["tmdb_key"] = o.tmdbKey
	}
	if len(body) == 1 {
		fail("usage: media-core-engine setup [--library DIR] [--name NAME] [--tmdb-key KEY]")
	}
	b, _ := json.Marshal(body)
	resp, err := cliCall("POST", adminURL()+"/api/setup/config", b, 30*time.Second)
	if err == nil {
		resp.Body.Close()
		fmt.Println("Saved. The server restarts to use it.")
		return
	}
	if waitHello(time.Second, "") {
		fail(err.Error()) // the server answered and said no
	}
	envPath := filepath.Join(ConfDir, "engine.env")
	if o.library != "" {
		lib := filepath.Clean(expandHome(o.library))
		for _, f := range libraryFolders {
			os.MkdirAll(filepath.Join(lib, f), 0o775)
		}
		if p := libraryProblem(lib); p != "" {
			fail(p)
		}
		if err := setEnvLine(envPath, "MEDIA_CORE_LIBRARY", lib); err != nil {
			fail(err.Error())
		}
	}
	if o.name != "" {
		if err := setEnvLine(envPath, "MEDIA_CORE_NAME", cleanName(o.name, "")); err != nil {
			fail(err.Error())
		}
	}
	if o.tmdbKey != "" {
		if err := checkTMDBKey(o.tmdbKey); err != nil {
			fail(err.Error())
		}
		os.MkdirAll(ConfDir, 0o700)
		if err := os.WriteFile(filepath.Join(ConfDir, "tmdb.key"), []byte(o.tmdbKey+"\n"), 0o600); err != nil {
			fail(err.Error())
		}
	}
	fmt.Println("Saved (the server wasn't running; it uses this when it starts).")
}

// ---------- upgrade ----------

func releaseBase(ver string) string {
	repo := envOr("MEDIA_CORE_REPO", defaultRepo)
	if ver == "" || ver == "latest" {
		return "https://github.com/" + repo + "/releases/latest/download"
	}
	if !strings.HasPrefix(ver, "v") {
		ver = "v" + ver
	}
	return "https://github.com/" + repo + "/releases/download/" + ver
}

func download(url string, max int64) ([]byte, error) {
	c := &http.Client{Timeout: 5 * time.Minute}
	r, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", url, r.Status)
	}
	return io.ReadAll(io.LimitReader(r.Body, max))
}

// sumFor finds file's hash in a SHA256SUMS text.
func sumFor(sums, file string) string {
	for _, l := range strings.Split(sums, "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == file {
			return f[0]
		}
	}
	return ""
}

// upgradeCmd replaces the binary with a release, checked against its SHA256SUMS. It refuses while something
// plays or downloads, keeps the old binary as .prev, and puts it back if the new one doesn't come up.
func upgradeCmd(args []string) {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	ver := fs.String("version", "latest", "the release to install, like v1.2.0")
	force := fs.Bool("force", false, "upgrade even while something plays or downloads")
	fs.Parse(args)
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		fail("upgrade works on Linux and macOS")
	}
	if !*force {
		resp, err := cliCall("GET", adminURL()+"/api/setup/busy", nil, 5*time.Second)
		if err == nil {
			var b struct{ Busy string }
			json.NewDecoder(resp.Body).Decode(&b)
			resp.Body.Close()
			if b.Busy != "" {
				fail("Not now: wait until " + b.Busy + " (or pass --force).")
			}
		}
	}
	file := "media-core-engine-" + runtime.GOOS + "-" + runtime.GOARCH
	base := releaseBase(*ver)
	sums, err := download(base+"/SHA256SUMS", 1<<20)
	if err != nil {
		fail("couldn't get the release's SHA256SUMS: " + err.Error())
	}
	want := sumFor(string(sums), file)
	if want == "" {
		fail("the release has no " + file)
	}
	bin, err := download(base+"/"+file, 200<<20)
	if err != nil {
		fail(err.Error())
	}
	got := sha256.Sum256(bin)
	if hex.EncodeToString(got[:]) != want {
		fail("the download doesn't match SHA256SUMS; nothing was changed")
	}
	dst := binPath()
	if exe, err := os.Executable(); err == nil {
		if e, err := filepath.EvalSymlinks(exe); err == nil {
			dst = e
		}
	}
	if err := os.WriteFile(dst+".new", bin, 0o755); err != nil {
		fail(err.Error())
	}
	out, err := exec.Command(dst+".new", "version").Output()
	if err != nil {
		os.Remove(dst + ".new")
		fail("the new binary doesn't run on this computer: " + err.Error())
	}
	newVer := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "media-core-engine"))
	if newVer == version {
		os.Remove(dst + ".new")
		fmt.Println("Already on", version)
		return
	}
	if err := os.Rename(dst, dst+".prev"); err != nil {
		fail(err.Error())
	}
	if err := os.Rename(dst+".new", dst); err != nil {
		os.Rename(dst+".prev", dst)
		fail(err.Error())
	}
	fmt.Printf("Upgrading %s -> %s\n", version, newVer)
	restartEngine()
	if waitHello(30*time.Second, newVer) {
		fmt.Println("Done. The old version is kept as", dst+".prev")
		// the new binary refreshes the service files it ships with
		cmd := exec.Command(dst, "install", "--yes")
		cmd.Stdout, cmd.Stderr = io.Discard, os.Stderr
		cmd.Run()
		return
	}
	fmt.Fprintln(os.Stderr, "The new version didn't come up; putting", version, "back.")
	os.Rename(dst, dst+".failed")
	os.Rename(dst+".prev", dst)
	restartEngine()
	os.Exit(2)
}

func restartEngine() {
	switch {
	case runtime.GOOS == "linux" && hasSystemd():
		exec.Command("systemctl", "--user", "restart", "media-core-engine.service").Run()
	case runtime.GOOS == "darwin":
		exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/app.mediacore.engine", os.Getuid())).Run()
	}
}

// ---------- uninstall ----------

// uninstallCmd stops and removes the services and the binary. --purge also deletes the settings, devices,
// progress and caches. The library is never touched.
func uninstallCmd(args []string) {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	purge := fs.Bool("purge", false, "also delete settings, paired devices, watch progress and caches")
	yes := fs.Bool("yes", false, "don't ask")
	fs.Parse(args)
	if *purge && !*yes {
		fmt.Printf("This deletes %s and %s: settings, paired devices and everyone's watch progress.\nYour library (%s) stays. Type delete to go on: ", ConfDir, DataDir, Media)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != "delete" {
			fail("Nothing was removed.")
		}
	}
	switch {
	case runtime.GOOS == "linux" && hasSystemd():
		exec.Command("systemctl", "--user", "disable", "--now", "media-core-engine.service", "media-core-aria2.service", "media-core-backup.timer").Run()
		for _, n := range unitNames {
			os.Remove(filepath.Join(unitDir(), n))
		}
		exec.Command("systemctl", "--user", "daemon-reload").Run()
	case runtime.GOOS == "darwin":
		exec.Command("launchctl", "unload", launchAgentPath()).Run()
		os.Remove(launchAgentPath())
	}
	for _, p := range []string{binPath(), binPath() + ".prev", binPath() + ".failed"} {
		os.Remove(p)
	}
	if *purge {
		os.RemoveAll(ConfDir)
		os.RemoveAll(DataDir)
		fmt.Println("Removed Media Core, its settings and its data. Your library is untouched:", Media)
		return
	}
	fmt.Printf("Removed Media Core. Settings and progress are still in %s and %s (uninstall --purge removes them).\nYour library is untouched: %s\n", ConfDir, DataDir, Media)
}
