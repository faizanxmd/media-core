package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// install sets this computer up as a server and starts it: the binary in ~/.local/bin, the settings, the
// downloader's config, the systemd user units. The one-line install (install.sh) downloads the binary and
// runs it; an AI agent runs the same command with --yes --json. It is safe to run again at any time: it
// never overwrites engine.env, aria2.conf, tmdb.key, devices.json or any data, and never touches the library.

const installUsage = `usage: media-core-engine install [--library DIR] [--make-folders] [--name NAME]
                                 [--tmdb-key KEY | --tmdb-key-file FILE]
                                 [--no-service] [--no-apps] [--yes] [--json]`

const macHint = "Media Core doesn't install itself as a service on this system (it needs Linux with systemd).\n" +
	"On a Mac, run it yourself:\n  media-core-engine setup --library DIR\n  media-core-engine serve"

const noSessionHint = "No systemd user session. Log in normally (not `su`), or set XDG_RUNTIME_DIR=/run/user/$(id -u), " +
	"or use --no-service and run `media-core-engine serve` yourself."

type installOpts struct {
	library, name, tmdbKey, tmdbKeyFile                string
	makeFolders, noService, noApps, yes, force, asJSON bool
}

type installResult struct {
	OK             bool              `json:"ok"`
	Error          string            `json:"error,omitempty"`
	Version        string            `json:"version"`
	Binary         string            `json:"binary"`
	Service        string            `json:"service"` // running, not-installed, failed
	URLs           []string          `json:"urls"`
	SetupURL       string            `json:"setup_url"`
	SetupCode      string            `json:"setup_code"`
	Claimed        bool              `json:"claimed"`
	Library        string            `json:"library"`
	LibraryProblem string            `json:"library_problem"`
	Apps           map[string]string `json:"apps"`
	NeedsSudo      []string          `json:"needs_sudo"`
	Warnings       []string          `json:"warnings"`
}

// configFlags: the settings flags install and setup share.
func configFlags(fs *flag.FlagSet, o *installOpts) {
	fs.StringVar(&o.library, "library", "", "the library folder (Movies, TV Shows and Music go in it)")
	fs.BoolVar(&o.makeFolders, "make-folders", false, "make the library folder and Movies, TV Shows, Music in it")
	fs.StringVar(&o.name, "name", "", "the server's name, as the apps show it")
	fs.StringVar(&o.tmdbKey, "tmdb-key", "", "a TMDB API key (prefer --tmdb-key-file or MEDIA_CORE_TMDB_KEY: a flag shows in shell history)")
	fs.StringVar(&o.tmdbKeyFile, "tmdb-key-file", "", "a file holding the TMDB API key")
	fs.BoolVar(&o.yes, "yes", false, "ask nothing; use the flags and defaults")
	fs.BoolVar(&o.asJSON, "json", false, "print one JSON object")
}

// tmdbKeyFrom: the key from --tmdb-key, else --tmdb-key-file, else MEDIA_CORE_TMDB_KEY ("" when none is given).
func tmdbKeyFrom(o installOpts) (string, *cliErr) {
	if o.tmdbKey != "" {
		return strings.TrimSpace(o.tmdbKey), nil
	}
	if o.tmdbKeyFile != "" {
		b, err := os.ReadFile(expandHome(o.tmdbKeyFile))
		if err != nil {
			return "", cliFail(exitFailed, "couldn't read the TMDB key file: %v", err)
		}
		if strings.TrimSpace(string(b)) == "" {
			return "", cliFail(exitFailed, "%s is empty", o.tmdbKeyFile)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(os.Getenv("MEDIA_CORE_TMDB_KEY")), nil
}

// configChangeFrom turns the flags into a settings change (nil fields for what wasn't given).
func configChangeFrom(o installOpts) (configChange, *cliErr) {
	in := configChange{MakeFolders: o.makeFolders}
	if o.library != "" {
		in.Library = &o.library
	}
	if o.name != "" {
		in.Name = &o.name
	}
	key, err := tmdbKeyFrom(o)
	if err != nil {
		return in, err
	}
	if key != "" {
		in.TMDBKey = &key
	}
	return in, nil
}

func installCmd(args []string) {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, installUsage) }
	var o installOpts
	configFlags(fs, &o)
	fs.BoolVar(&o.noService, "no-service", false, "don't set up the systemd service; you run `media-core-engine serve`")
	fs.BoolVar(&o.noApps, "no-apps", false, "don't fetch the TV and phone apps")
	fs.BoolVar(&o.force, "force", false, "restart even while something plays or downloads")
	if fs.Parse(args) != nil || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, installUsage)
		os.Exit(exitUsage)
	}
	res, err := runInstall(o, sayTo(o.asJSON))
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

func runInstall(o installOpts, say func(string, ...any)) (*installResult, *cliErr) {
	if os.Getuid() == 0 {
		return nil, cliFail(exitUnsupported, "Don't install Media Core as root. It runs as your own user: log in as that user and run this again.")
	}
	if runtime.GOOS != "linux" {
		return nil, cliFail(exitUnsupported, "%s", macHint)
	}
	if !o.noService {
		if !hasSystemd() {
			return nil, cliFail(exitUnsupported, "%s", noSessionHint)
		}
	}
	if err := os.MkdirAll(ConfDir, 0o700); err != nil {
		return nil, cliFail(exitFailed, "%v", err)
	}
	os.Chmod(ConfDir, 0o700)
	if err := os.MkdirAll(DataDir, 0o700); err != nil {
		return nil, cliFail(exitFailed, "%v", err)
	}

	// the binary, in its place
	bin, err := selfExe()
	if err != nil {
		return nil, cliFail(exitFailed, "%v", err)
	}
	if dst := binPath(); bin != dst {
		a, errA := os.Stat(bin)
		b, errB := os.Stat(dst)
		if errA != nil || errB != nil || !os.SameFile(a, b) {
			if err := copyExecutable(bin, dst); err != nil {
				return nil, cliFail(exitFailed, "couldn't put the binary at %s: %v", dst, err)
			}
		}
		bin = dst
	}
	res := &installResult{Version: version, Binary: bin, Service: "not-installed", URLs: []string{}, Apps: map[string]string{},
		NeedsSudo: []string{}, Warnings: []string{}}
	warn := func(format string, a ...any) { res.Warnings = append(res.Warnings, fmt.Sprintf(format, a...)) }

	// the settings, exactly as `setup` applies them
	in, cerr := configChangeFrom(o)
	if cerr != nil {
		return nil, cerr
	}
	envPath := filepath.Join(ConfDir, "engine.env")
	if in.Library == nil && envOrFrom("MEDIA_CORE_LIBRARY", "", envPath) == "" && libraryProblem(Media) != "" && !o.yes && stdinTTY() {
		rd := bufio.NewReader(os.Stdin)
		lib := expandHome(ask(rd, "Where is your media?", Media))
		in.Library = &lib
		if fi, err := os.Stat(lib); err != nil || !fi.IsDir() || len(missingFolders(lib)) > 0 {
			in.MakeFolders = askYesIn(rd, "Make "+lib+" with Movies, TV Shows and Music folders in it?")
		}
	}
	cfg, cerr := applyConfig(in)
	if cerr != nil {
		if in.Library != nil && !in.MakeFolders && cerr.code == exitFailed {
			cerr.msg += "\nMake the folder first, or add --make-folders."
		}
		return nil, cerr
	}
	if cfg.Library != Media {
		setPaths(ConfDir, DataDir, cfg.Library)
	}
	res.Library, res.LibraryProblem = Media, libraryProblem(Media)

	// the downloader's settings: only written when missing
	port := 6800
	if _, err := os.Stat(Aria2Conf); err != nil {
		if portTaken(port) { // something else has aria2's usual port
			if p, err := freePort(); err == nil {
				port = p
			}
		}
	}
	if err := ensureAria2Conf(port); err != nil {
		return nil, cliFail(exitFailed, "%v", err)
	}

	var failed *cliErr
	if !o.noService {
		failed = installService(o, bin, res, say, warn)
	}

	// what needs the owner, and sudo: missing tools, staying up after logout
	sudo, byHand := missingTools()
	res.NeedsSudo = append(sudo, res.NeedsSudo...)
	res.Warnings = append(res.Warnings, byHand...)
	if failed != nil {
		return res, failed
	}

	if !o.noApps {
		if tag := releaseVersion(); tag != "" && !appsAt(version) {
			say("Fetching the TV and phone apps…")
			if err := fetchApps(tag); err != nil {
				warn("The apps couldn't be fetched (%v). Later: %s upgrade --apps", err, cmdName(bin))
			}
		}
	}
	res.Apps = appVersions()

	if d, err := loadDevices(filepath.Join(ConfDir, "devices.json")); err != nil {
		warn("%v", err)
	} else {
		res.Claimed = d.claimed()
	}
	res.URLs = siteURLs()
	res.SetupURL = res.URLs[0] + "#/setup"
	// the code is made only once the server runs: a server that starts later makes its own and drops this one
	if !res.Claimed && res.Service == "running" {
		code, err := newSetupCode(filepath.Join(ConfDir, "setup.code"))
		if err != nil {
			warn("couldn't make a setup code (%v). Try: %s setup-code", err, cmdName(bin))
		}
		res.SetupCode = code
	}
	res.OK = true
	printInstallSummary(res, say)
	return res, nil
}

// installService writes the units, starts the services and waits for the server. It fills res.Service and
// the linger line in res.NeedsSudo.
func installService(o installOpts, bin string, res *installResult, say func(string, ...any), warn func(string, ...any)) *cliErr {
	changed, err := writeUnits(unitDir(), bin)
	if err != nil {
		return cliFail(exitFailed, "couldn't write the service files: %v", err)
	}
	for _, n := range changed {
		say("wrote %s", filepath.Join(unitDir(), n))
	}
	hasAria := fileExists(filepath.Join(unitDir(), aria2Unit))
	if out, err := runSystemctl("daemon-reload"); err != nil {
		return cliFail(exitFailed, "systemctl --user daemon-reload: %v %s", err, out)
	}
	enable := []string{"enable", engineUnit}
	if hasAria {
		enable = append(enable, aria2Unit)
	}
	if out, err := runSystemctl(enable...); err != nil {
		return cliFail(exitFailed, "systemctl --user %s: %v %s", strings.Join(enable, " "), err, out)
	}
	if out, err := runSystemctl("enable", "--now", backupTimer); err != nil {
		warn("the nightly backup's timer didn't start (%v %s)", err, out)
	}

	// restart only when there is something new to run (a changed unit, another version, or nothing
	// running), and never while something plays or downloads
	running, up := helloVersion()
	active, _ := runSystemctl("is-active", engineUnit)
	needed := len(changed) > 0 || !up || running != version || active != "active"
	why := ""
	if needed && up && !o.force {
		why, _ = serverBusy()
	}
	want := version
	switch {
	case !needed:
	case why != "":
		want = "" // the old server keeps running; any answer will do
		warn("The server wasn't restarted, so it still runs what it ran before. Wait until %s, then: systemctl --user restart %s", why, engineUnit)
	default:
		if hasAria {
			if a, _ := runSystemctl("is-active", aria2Unit); a != "active" || contains(changed, aria2Unit) {
				if out, err := runSystemctl("restart", aria2Unit); err != nil {
					warn("the downloader's service didn't start (%v %s)", err, out)
				}
			}
		}
		if out, err := runSystemctl("restart", engineUnit); err != nil {
			res.Service = "failed"
			return cliFail(exitFailed, "the server's service didn't start: %v %s\n%s", err, out, journalTail(engineUnit, 20))
		}
	}
	if !waitHello(helloWait, want) {
		res.Service = "failed"
		return cliFail(exitFailed, "The server didn't answer within %s. Its last log lines:\n%s", helloWait, journalTail(engineUnit, 20))
	}
	res.Service = "running"

	// staying up after logout
	if name := userName(); name != "" {
		if out, _ := runLoginctl("show-user", name, "-p", "Linger", "--value"); strings.TrimSpace(out) != "yes" {
			if _, err := runLoginctl("enable-linger"); err != nil {
				res.NeedsSudo = append(res.NeedsSudo, "sudo loginctl enable-linger "+name)
			}
		}
	}
	return nil
}

func printInstallSummary(res *installResult, say func(string, ...any)) {
	me := cmdName(res.Binary)
	say("")
	switch {
	case res.Service != "running":
		say("Media Core %s is set up, without a service. Start it yourself:\n  %s serve", res.Version, me)
		if !res.Claimed {
			say("It prints a setup code when it starts. Open one of these at home and enter it:")
			for _, u := range res.URLs {
				say("  %s#/setup", u)
			}
		}
	case res.Claimed:
		say("Media Core %s is running:", res.Version)
		for _, u := range res.URLs {
			say("  %s", u)
		}
	default:
		say("Media Core %s is running. To finish, open this on a computer or phone at home:", res.Version)
		for _, u := range res.URLs {
			say("  %s#/setup", u)
		}
		say("Setup code: %s   (lasts 24 hours; `%s setup-code` makes a new one)", res.SetupCode, me)
	}
	if res.LibraryProblem != "" {
		say("\nThe library isn't ready: %s\nPick the folder on the setup page, or: %s setup --library DIR --make-folders", res.LibraryProblem, me)
	}
	if len(res.NeedsSudo) > 0 {
		say("\nStill to do, for you to run:")
		for _, l := range res.NeedsSudo {
			say("  %s", l)
		}
	}
	for _, w := range res.Warnings {
		say("\nNote: %s", w)
	}
	if me != "media-core-engine" {
		dir := filepath.Dir(res.Binary)
		say("\n%s isn't on your PATH. Add this to your shell's profile:\n  export PATH=\"%s:$PATH\"", dir, homeRel(dir))
	}
	say("\nCheck it any time: %s doctor", me)
}

// homeRel writes a path under the home folder with $HOME, for a line someone pastes into a profile.
func homeRel(p string) string {
	if h := homeDir(); strings.HasPrefix(p, h+"/") {
		return "$HOME" + strings.TrimPrefix(p, h)
	}
	return p
}

// missingTools: one install command for every missing tool this system's package manager has (the tools
// named after it), and a line for each tool that has to be fetched by hand.
func missingTools() (sudo, byHand []string) {
	var pkgs, ids []string
	pm := pkgManager()
	for _, t := range tools {
		if toolPath(t.ID) != "" {
			continue
		}
		if pm != "" && t.Pkg[pm] != "" {
			pkgs = append(pkgs, strings.Fields(t.Pkg[pm])...)
			ids = append(ids, t.ID)
		} else {
			byHand = append(byHand, t.ID+" is missing ("+t.Feature+"): "+installHint(t.ID))
		}
	}
	sudo = []string{}
	if len(pkgs) > 0 {
		sudo = append(sudo, pkgInstall[pm]+" "+strings.Join(uniq(pkgs), " ")+"   # "+strings.Join(ids, ", "))
	}
	return sudo, byHand
}

// portTaken: something on this computer already listens on a localhost port.
var portTaken = func(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func missingFolders(lib string) []string {
	var missing []string
	for _, f := range libraryFolders {
		if fi, err := os.Stat(filepath.Join(lib, f)); err != nil || !fi.IsDir() {
			missing = append(missing, f)
		}
	}
	return missing
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
