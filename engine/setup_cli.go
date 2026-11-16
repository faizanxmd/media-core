package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
)

// setup changes the library folder, the server's name or the TMDB key from the command line. It writes the
// same files the site's Settings page writes, through the same checks (applyConfig), then restarts the
// service if something changed. While something plays or downloads it changes nothing and exits 3.

const setupUsage = `usage: media-core-engine setup [--library DIR] [--make-folders] [--name NAME]
                               [--tmdb-key KEY | --tmdb-key-file FILE] [--yes] [--json]
The TMDB key can also come from the MEDIA_CORE_TMDB_KEY environment variable. Prefer that or
--tmdb-key-file: a key given as a flag shows up in shell history and in ps.`

type setupResult struct {
	OK        bool     `json:"ok"`
	Changed   []string `json:"changed"`
	Restarted bool     `json:"restarted"`
	Library   string   `json:"library"`
	Name      string   `json:"name"`
	TMDB      bool     `json:"tmdb"`
}

func setupCmd(args []string) {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, setupUsage) }
	var o installOpts
	configFlags(fs, &o)
	if fs.Parse(args) != nil || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, setupUsage)
		os.Exit(exitUsage)
	}
	say := sayTo(o.asJSON)
	res, err := runSetup(o)
	if err != nil {
		failWith(err.code, o.asJSON, err.msg)
	}
	switch {
	case len(res.Changed) == 0:
		say("Nothing to change: the server already has these settings.")
	case res.Restarted:
		say("Saved, and the server restarted to use it.")
	default:
		say("Saved. The server uses it the next time it starts.")
	}
	if o.asJSON {
		printJSON(res)
	}
}

func runSetup(o installOpts) (*setupResult, *cliErr) {
	in, err := configChangeFrom(o)
	if err != nil {
		return nil, err
	}
	if in.Library == nil && in.Name == nil && in.TMDBKey == nil && !in.MakeFolders {
		if o.yes || !stdinTTY() {
			return nil, cliFail(exitUsage, "%s", setupUsage)
		}
		// no flags, and someone to ask: the three questions
		rd := bufio.NewReader(os.Stdin)
		lib := expandHome(ask(rd, "Library folder (Movies, TV Shows and Music go in it)", Media))
		in.Library = &lib
		if len(missingFolders(lib)) > 0 {
			in.MakeFolders = askYesIn(rd, "Make "+lib+" with Movies, TV Shows and Music folders in it?")
		}
		name := ask(rd, "The server's name", serverName())
		in.Name = &name
		if key := ask(rd, "TMDB key, for posters and details (Enter keeps what's saved)", ""); key != "" {
			in.TMDBKey = &key
		}
	}
	up := false
	in.busy = func() string {
		why, answered := serverBusy()
		up = answered
		return why
	}
	cfg, err := applyConfig(in)
	if err != nil {
		if in.Library != nil && !in.MakeFolders && err.code == exitFailed && libraryProblem(expandHome(*in.Library)) != "" {
			err.msg += "\nMake the folder first, or add --make-folders."
		}
		return nil, err
	}
	res := &setupResult{OK: true, Changed: cfg.Changed, Library: cfg.Library, Name: cfg.Name, TMDB: cfg.TMDB}
	if res.Changed == nil {
		res.Changed = []string{}
	}
	if len(res.Changed) == 0 {
		return res, nil
	}
	if hasSystemd() {
		if active, _ := runSystemctl("is-active", engineUnit); active == "active" {
			if out, rerr := runSystemctl("restart", engineUnit); rerr != nil {
				return nil, cliFail(exitFailed, "Saved, but the server didn't restart: %v %s", rerr, out)
			}
			res.Restarted = true
			return res, nil
		}
	}
	if up {
		fmt.Fprintln(os.Stderr, "A server is running, but not as the installed service: restart it yourself to use the new settings.")
	}
	return res, nil
}
