package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// upgrade replaces the binary with a release, checked against its SHA256SUMS, and puts the old one back if
// the new one doesn't come up. It refuses while something plays or downloads. engine.env, devices.json and
// all data are left alone; the unit files are rewritten by the new binary's own `install`.

const upgradeUsage = `usage: media-core-engine upgrade [--version vX.Y.Z] [--apps] [--check] [--force] [--json]`

const repoSlug = "faizanxmd/media-core"

// releaseBase: where releases live. MEDIA_CORE_BASE_URL replaces it (tests, mirrors) with a folder laid out
// as <base>/latest (a text file holding the tag) and <base>/download/<tag>/<asset>.
func releaseBase() (base string, custom bool) {
	if u := envOr("MEDIA_CORE_BASE_URL", ""); u != "" {
		return strings.TrimRight(u, "/"), true
	}
	return "https://github.com/" + envOr("MEDIA_CORE_REPO", repoSlug) + "/releases", false
}

func assetURL(tag, name string) string {
	base, _ := releaseBase()
	return base + "/download/" + tag + "/" + name
}

// tagOf: "1.2.3" or "v1.2.3" as the tag "v1.2.3".
func tagOf(v string) string {
	v = strings.TrimSpace(v)
	if v != "" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

func validTag(tag string) bool {
	if len(tag) < 2 || len(tag) > 64 || tag[0] != 'v' {
		return false
	}
	return strings.IndexFunc(tag, func(r rune) bool {
		return !(r == '.' || r == '-' || r == '+' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) < 0
}

// latestTag asks which release is the latest. On GitHub that is the Location of releases/latest, read with
// redirects off: no API token and no rate limit.
func latestTag() (string, error) {
	base, custom := releaseBase()
	tag := ""
	if custom {
		b, err := download(base+"/latest", 1<<10)
		if err != nil {
			return "", err
		}
		tag = strings.TrimSpace(string(b))
	} else {
		c := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		r, err := c.Get(base + "/latest")
		if err != nil {
			return "", fmt.Errorf("couldn't reach the release page (%v)", err)
		}
		r.Body.Close()
		loc := r.Header.Get("Location")
		if i := strings.Index(loc, "/releases/tag/"); i >= 0 {
			tag = path.Base(loc[i:])
		}
		if tag == "" {
			return "", fmt.Errorf("%s/latest names no release (HTTP %d): nothing has been released yet", base, r.StatusCode)
		}
	}
	if !validTag(tag) {
		return "", fmt.Errorf("the release host named an odd version: %q", tag)
	}
	return tag, nil
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

// downloadTo saves url as dst and returns the SHA-256 of what arrived.
func downloadTo(url, dst string, mode os.FileMode) (string, error) {
	c := &http.Client{Timeout: 30 * time.Minute}
	r, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return "", fmt.Errorf("%s: %s", url, r.Status)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), r.Body)
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		os.Remove(dst)
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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

type upgradeOpts struct {
	version                    string
	apps, check, force, asJSON bool
}

type upgradeResult struct {
	OK         bool              `json:"ok"`
	Error      string            `json:"error,omitempty"`
	From       string            `json:"from"`
	To         string            `json:"to"`
	RolledBack bool              `json:"rolled_back"`
	Apps       map[string]string `json:"apps"`
	Backup     string            `json:"backup"`
	Warnings   []string          `json:"warnings,omitempty"`
}

func upgradeCmd(args []string) {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, upgradeUsage) }
	var o upgradeOpts
	fs.StringVar(&o.version, "version", "", "the release to install, like v1.2.0 (default: the latest)")
	fs.BoolVar(&o.apps, "apps", false, "fetch the TV and phone apps too, even when the engine is already on that version")
	fs.BoolVar(&o.check, "check", false, "only say whether a newer release exists")
	fs.BoolVar(&o.force, "force", false, "upgrade even while something plays or downloads")
	fs.BoolVar(&o.asJSON, "json", false, "print one JSON object")
	if fs.Parse(args) != nil || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, upgradeUsage)
		os.Exit(exitUsage)
	}
	say := sayTo(o.asJSON)
	if o.check {
		tag, err := latestTag()
		if err != nil {
			failWith(exitFailed, o.asJSON, err.Error())
		}
		latest := strings.TrimPrefix(tag, "v")
		if latest == version {
			say("Media Core %s is the latest release.", version)
		} else {
			say("This is Media Core %s; the latest release is %s. To move to it: media-core-engine upgrade", version, latest)
		}
		if o.asJSON {
			printJSON(map[string]any{"current": version, "latest": latest, "update": latest != version})
		}
		return
	}
	res, err := runUpgrade(o, say)
	if err != nil {
		if res == nil || !o.asJSON {
			failWith(err.code, o.asJSON, err.msg)
		}
		res.Error = err.msg
		printJSON(res)
		os.Exit(err.code)
	}
	if o.asJSON {
		printJSON(res)
	}
}

func runUpgrade(o upgradeOpts, say func(string, ...any)) (*upgradeResult, *cliErr) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil, cliFail(exitUnsupported, "upgrade works on Linux and macOS")
	}
	// 1. which release
	tag := tagOf(o.version)
	if tag == "" {
		t, err := latestTag()
		if err != nil {
			return nil, cliFail(exitFailed, "%v", err)
		}
		tag = t
	} else if !validTag(tag) {
		return nil, cliFail(exitUsage, "--version takes a release like v1.2.0")
	}
	target := strings.TrimPrefix(tag, "v")
	res := &upgradeResult{From: version, To: target, Apps: map[string]string{}}
	warn := func(format string, a ...any) {
		res.Warnings = append(res.Warnings, fmt.Sprintf(format, a...))
		say("Note: "+format, a...)
	}
	apps := func() {
		say("Fetching the TV and phone apps…")
		if err := fetchApps(tag); err != nil {
			warn("the apps couldn't be fetched (%v). Later: media-core-engine upgrade --apps", err)
		}
		res.Apps = appVersions()
	}

	// 3. nothing to do?
	if target == version {
		if o.apps {
			apps()
			if len(res.Warnings) > 0 {
				return res, cliFail(exitFailed, "%s", res.Warnings[0])
			}
			say("The apps are from %s now. The engine was already on it.", target)
		} else {
			say("Already on %s.", version)
			res.Apps = appVersions()
		}
		res.OK = true
		return res, nil
	}

	// 4. never under something that is playing or downloading
	if !o.force {
		if why, _ := serverBusy(); why != "" {
			return nil, cliFail(exitBusy, "Not now: wait until %s (or pass --force).", why)
		}
	}

	// 5. the new binary, checked
	bin, err := selfExe()
	if err != nil {
		return nil, cliFail(exitFailed, "%v", err)
	}
	asset := "media-core-engine-" + runtime.GOOS + "-" + runtime.GOARCH
	sums, err := download(assetURL(tag, "SHA256SUMS"), 1<<20)
	if err != nil {
		return nil, cliFail(exitFailed, "couldn't get %s's SHA256SUMS: %v", tag, err)
	}
	want := sumFor(string(sums), asset)
	if want == "" {
		return nil, cliFail(exitFailed, "release %s has no %s", tag, asset)
	}
	fresh := filepath.Join(filepath.Dir(bin), ".media-core-engine.new")
	say("Downloading %s %s…", asset, tag)
	got, err := downloadTo(assetURL(tag, asset), fresh, 0o755)
	if err != nil {
		return nil, cliFail(exitFailed, "%v", err)
	}
	if got != want {
		os.Remove(fresh)
		return nil, cliFail(exitFailed, "The download doesn't match SHA256SUMS (got %s, the release says %s). Nothing was changed.", got, want)
	}
	// 6. it runs here, and it is the version asked for
	out, err := exec.Command(fresh, "version").Output()
	if said := strings.TrimSpace(string(out)); err != nil || said != "media-core-engine "+target {
		os.Remove(fresh)
		if err != nil {
			return nil, cliFail(exitFailed, "The new binary doesn't run on this computer (%v). Nothing was changed.", err)
		}
		return nil, cliFail(exitFailed, "The new binary says it is %q, not %s. Nothing was changed.", said, target)
	}

	// 7. a backup of the state, before anything is swapped
	schemaBefore, _, _ := readSchema()
	bk := filepath.Join(backupDir(), fmt.Sprintf("upgrade-%s-%d.tgz", version, time.Now().Unix()))
	if _, err := writeBackup(bk); err != nil {
		have := false
		for _, e := range backupEntries() {
			have = have || fileExists(filepath.Join(e.base, e.rel))
		}
		if have {
			os.Remove(fresh)
			return nil, cliFail(exitFailed, "Couldn't back up before upgrading (%v). Nothing was changed.", err)
		}
		bk = "" // a server with no state yet
	} else {
		res.Backup = bk
		pruneBackups(backupDir(), "upgrade-", 5)
	}

	// 8. swap, keeping the old binary
	prev := bin + ".prev"
	if err := copyExecutable(bin, prev); err != nil {
		os.Remove(fresh)
		return nil, cliFail(exitFailed, "Couldn't keep the old binary as %s (%v). Nothing was changed.", prev, err)
	}
	if err := renameFile(fresh, bin); err != nil {
		os.Remove(fresh)
		return nil, cliFail(exitFailed, "Couldn't put the new binary in place (%v). Nothing was changed.", err)
	}
	say("Upgrading %s -> %s", version, target)

	// 9. the new binary sets itself up: units, restart; 10. and must answer as the new version
	if !serviceInstalled() {
		apps()
		res.OK = true
		say("Done: %s is Media Core %s now, and the old one is kept as %s. Nothing here runs it as a service: restart the server to use it.", bin, target, prev)
		return res, nil
	}
	// (install restarts the service itself, once, because the running version differs; nothing here does)
	why := ""
	said, ierr := installWith(bin)
	if ierr != nil {
		why = fmt.Sprintf("the new version's install failed (%v):\n%s", ierr, said)
	} else if !waitHello(healthWait, target) {
		why = fmt.Sprintf("the new version didn't answer within %s", healthWait)
	} else if said != "" {
		say("%s", said) // its "still to do" lines and notes
	}
	if why == "" {
		apps()
		res.OK = true
		say("Done: Media Core %s. The old version is kept as %s", target, prev)
		return res, nil
	}

	// 11. back to the old one
	say("The upgrade failed: %s", why)
	logs := journalTail(engineUnit, 30)
	res.RolledBack = true
	if err := rollBack(bin, prev, bk, schemaBefore); err != nil {
		res.RolledBack = false
		return res, cliFail(exitFailed, "The upgrade to %s failed (%s), and so did going back: %v\n"+
			"%s should be Media Core %s; %s, if it is still there, is the copy of %s made before the upgrade. "+
			"To finish by hand: cp %s %s && systemctl --user restart %s\nThe state backup from before the upgrade: %s\n\n%s",
			target, why, err, bin, version, prev, version, prev, bin, engineUnit, bk, logs)
	}
	return res, cliFail(exitPartial, "The upgrade to %s failed: %s.\nMedia Core %s is running again; nothing was lost. The failed start's last log lines:\n\n%s",
		target, why, version, logs)
}

// rollBack puts the old binary back and starts it. If the new version had already migrated the state to a
// newer schema, the backup made before the upgrade is restored first, with the engine stopped.
func rollBack(bin, prev, backup string, schemaBefore int) error {
	if err := renameFile(prev, bin); err != nil {
		return fmt.Errorf("couldn't move %s back over %s: %v", prev, bin, err)
	}
	if now, _, _ := readSchema(); now > schemaBefore {
		if backup == "" {
			return fmt.Errorf("the state was migrated to schema %d and there is no backup to go back to", now)
		}
		runSystemctl("stop", engineUnit)
		if err := restoreBackup(backup); err != nil {
			return fmt.Errorf("couldn't restore %s: %v", backup, err)
		}
	}
	// the old binary's own install rewrites its units and restarts; an old one that can't is just restarted
	if _, err := installWith(bin); err != nil {
		if hasSystemd() {
			if out, err := runSystemctl("restart", engineUnit); err != nil {
				return fmt.Errorf("the old version didn't start (%v %s)", err, out)
			}
		}
	}
	if !waitHello(healthWait, version) {
		return fmt.Errorf("the old version (%s) didn't answer within %s", version, healthWait)
	}
	return nil
}

// serviceInstalled: this server runs as the systemd user service that install set up.
var serviceInstalled = func() bool {
	return runtime.GOOS == "linux" && hasSystemd() && fileExists(filepath.Join(unitDir(), engineUnit))
}

// installWith runs a binary's own `install`, which rewrites the units and restarts the service through the
// same code as a first install. The busy check was made (or waived) by the caller, so it isn't repeated.
func installWith(bin string) (string, error) {
	cmd := exec.Command(bin, "install", "--no-apps", "--yes", "--force")
	cmd.Env = append(os.Environ(), "MEDIA_CORE_INSTALL_DIR="+filepath.Dir(bin))
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// restoreBackup writes an archive's files back where they belong. The engine must be stopped.
func restoreBackup(p string) error {
	files, _, err := readBackup(p)
	if err != nil {
		return err
	}
	for _, e := range backupEntries() {
		b, ok := files[e.name]
		if !ok {
			continue
		}
		dst := filepath.Join(e.base, e.rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(dst+".tmp", b, 0o600); err != nil {
			return err
		}
		if err := os.Rename(dst+".tmp", dst); err != nil {
			return err
		}
	}
	return nil
}

// ---------- the apps ----------

// appFiles: what a release carries for the apps, each APK before its .json.
var appFiles = []string{"media-core.apk", "media-core.json", "media-core-phone.apk", "media-core-phone.json"}

// fetchApps puts the TV and phone apps from a release into the data folder, where the server hands them to
// devices. Every file is checked against the release's SHA256SUMS (older releases: APPS.SHA256SUMS) before
// any is written. Each APK lands before its .json, because the .json is what makes the apps offer the
// update: a device must never see a version whose APK isn't there yet.
func fetchApps(tag string) error {
	if tag == "" {
		t, err := latestTag()
		if err != nil {
			return err
		}
		tag = t
	}
	var sums string
	for _, name := range []string{"SHA256SUMS", "APPS.SHA256SUMS"} {
		if b, err := download(assetURL(tag, name), 1<<20); err == nil && sumFor(string(b), appFiles[0]) != "" {
			sums = string(b)
			break
		}
	}
	if sums == "" {
		return fmt.Errorf("release %s has no apps", tag)
	}
	dir := filepath.Join(DataDir, "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := func(f string) string { return filepath.Join(dir, "."+f+".new") }
	defer func() {
		for _, f := range appFiles {
			os.Remove(tmp(f))
		}
	}()
	for _, f := range appFiles {
		want := sumFor(sums, f)
		if want == "" {
			return fmt.Errorf("release %s's checksums have no %s", tag, f)
		}
		got, err := downloadTo(assetURL(tag, f), tmp(f), 0o644)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s doesn't match the release's checksums", f)
		}
	}
	for _, f := range appFiles {
		if err := renameFile(tmp(f), filepath.Join(dir, f)); err != nil {
			return err
		}
	}
	return nil
}

// appVersions: the app builds this server hands out, as {"tv": "1.0.0", "phone": "1.0.0"}.
func appVersions() map[string]string {
	out := map[string]string{}
	for key, name := range map[string]string{"tv": "media-core", "phone": "media-core-phone"} {
		if a := readAppInfo(name); a != nil {
			out[key] = a.VersionName
		}
	}
	return out
}

// appsAt: both apps are here, at version v.
func appsAt(v string) bool {
	a := appVersions()
	return a["tv"] == v && a["phone"] == v
}

// releaseVersion: the release this binary came from ("v1.2.3"), or "" for a build from source.
func releaseVersion() string {
	v := version
	if v == "" || strings.HasPrefix(v, "dev") || strings.Contains(v, "test") || !strings.ContainsAny(v[:1], "0123456789") {
		return ""
	}
	return "v" + v
}

// appsCmd: media-core-engine apps [--version vX.Y.Z]: the same as upgrade --apps, without touching the engine.
func appsCmd(args []string) {
	fs := flag.NewFlagSet("apps", flag.ContinueOnError)
	ver := fs.String("version", "", "the release to take the apps from (default: this engine's own, else the latest)")
	asJSON := fs.Bool("json", false, "print one JSON object")
	if fs.Parse(args) != nil || fs.NArg() > 0 {
		os.Exit(exitUsage)
	}
	tag := tagOf(*ver)
	if tag == "" {
		tag = releaseVersion()
	} else if !validTag(tag) {
		failWith(exitUsage, *asJSON, "--version takes a release like v1.2.0")
	}
	if err := fetchApps(tag); err != nil {
		failWith(exitFailed, *asJSON, "apps: "+err.Error())
	}
	sayTo(*asJSON)("The TV and phone apps are ready: devices offer the update, and the server's apps page has them.")
	if *asJSON {
		printJSON(map[string]any{"ok": true, "apps": appVersions()})
	}
}
