package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// uninstall stops and removes the services and the binary. The settings and the data stay unless --purge is
// given. The library is never opened, listed or changed, whatever the flags.

const uninstallUsage = `usage: media-core-engine uninstall [--purge] [--yes] [--force] [--json]`

type uninstallOpts struct {
	purge, yes, force, asJSON bool
}

type uninstallResult struct {
	OK      bool     `json:"ok"`
	Removed []string `json:"removed"`
	Kept    []string `json:"kept"`
	Purged  bool     `json:"purged"`
	Library string   `json:"library"`
}

func uninstallCmd(args []string) {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, uninstallUsage) }
	var o uninstallOpts
	fs.BoolVar(&o.purge, "purge", false, "also delete settings, paired devices, watch progress and caches")
	fs.BoolVar(&o.yes, "yes", false, "don't ask")
	fs.BoolVar(&o.force, "force", false, "with --yes: go on even while something plays or downloads")
	fs.BoolVar(&o.asJSON, "json", false, "print one JSON object")
	if fs.Parse(args) != nil || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, uninstallUsage)
		os.Exit(exitUsage)
	}
	res, err := runUninstall(o, sayTo(o.asJSON), askYes)
	if err != nil {
		failWith(err.code, o.asJSON, err.msg)
	}
	if o.asJSON {
		printJSON(res)
	}
}

func runUninstall(o uninstallOpts, say func(string, ...any), confirm func(string) bool) (*uninstallResult, *cliErr) {
	if o.purge && (within(Media, ConfDir) || within(Media, DataDir)) {
		return nil, cliFail(exitFailed, "The library (%s) is inside Media Core's own folders, and --purge would delete it. Move it out first, or uninstall without --purge.", Media)
	}
	// what goes
	bins := []string{binPath(), binPath() + ".prev"}
	if exe, err := selfExe(); err == nil && filepath.Base(exe) == "media-core-engine" {
		bins = append(bins, exe, exe+".prev", filepath.Join(filepath.Dir(exe), ".media-core-engine.new"))
	}
	var targets []string
	for _, n := range unitNames {
		targets = append(targets, filepath.Join(unitDir(), n))
	}
	targets = uniq(append(targets, bins...))
	newest := newestBackup()

	if !o.yes {
		if !stdinTTY() {
			return nil, cliFail(exitUsage, "uninstall asks before it removes anything, and there's no terminal to ask on. Add --yes.\n%s", uninstallUsage)
		}
		say("This stops Media Core and removes:")
		for _, t := range targets {
			if fileExists(t) {
				say("  %s", t)
			}
		}
		if o.purge {
			say("  %s   (settings and paired devices)\n  %s   (everyone's watch progress, backups, caches)", ConfDir, DataDir)
			if newest != "" {
				say("The newest backup is %s. Copy it somewhere else first if you may want it.", newest)
			}
		}
		say("Your library at %s is not touched.", Media)
		if !confirm("Go on?") {
			return nil, cliFail(exitFailed, "Nothing was removed.")
		}
	}
	if !(o.yes && o.force) {
		if why, _ := serverBusy(); why != "" {
			return nil, cliFail(exitBusy, "Not now: wait until %s (or pass --yes --force).", why)
		}
	}

	res := &uninstallResult{Removed: []string{}, Kept: []string{}, Purged: o.purge, Library: Media}
	if hasSystemd() {
		for _, n := range []string{backupTimer, backupUnit, engineUnit, aria2Unit} {
			runSystemctl("disable", "--now", n) // one that was never installed just says so
		}
	}
	remove := func(p string) {
		if !fileExists(p) {
			return
		}
		if err := os.Remove(p); err != nil {
			say("couldn't remove %s: %v", p, err)
			res.Kept = append(res.Kept, p)
			return
		}
		res.Removed = append(res.Removed, p)
	}
	for _, t := range targets {
		remove(t) // on Linux a running binary can delete its own file
	}
	if hasSystemd() {
		runSystemctl("daemon-reload")
	}
	if o.purge {
		if newest != "" {
			say("The newest backup was %s; it goes with the data folder.", newest)
		}
		if dir := envOr("MEDIA_CORE_BACKUP_DIR", ""); dir != "" {
			say("The backup copies in %s are left alone.", filepath.Join(dir, "media-core-backup"))
		}
		for _, d := range []string{ConfDir, DataDir} {
			if err := os.RemoveAll(d); err != nil {
				return nil, cliFail(exitFailed, "couldn't remove %s: %v", d, err)
			}
			res.Removed = append(res.Removed, d)
		}
		say("Removed Media Core, its settings and its data.")
	} else {
		res.Kept = append(res.Kept, ConfDir, DataDir)
		say("Removed Media Core. Your settings, devices and watch progress are still here:\n  %s\n  %s\nTo remove them too: rm -rf %s %s", ConfDir, DataDir, shellQuote(ConfDir), shellQuote(DataDir))
	}
	say("Your library at %s was not touched.", Media)
	if hasSystemd() {
		say("If nothing else of yours has to keep running after you log out: sudo loginctl disable-linger %s", userName())
	}
	res.OK = true
	return res, nil
}

// newestBackup: the newest archive in the backup folder ("" when there is none).
func newestBackup() string {
	names, _ := filepath.Glob(filepath.Join(backupDir(), "*.tgz"))
	if len(names) == 0 {
		return ""
	}
	sort.Slice(names, func(i, j int) bool { return backupTime(names[i]).After(backupTime(names[j])) })
	return names[0]
}
