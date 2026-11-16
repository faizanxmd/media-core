package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// The systemd user units, carried in the binary so there is one copy of them. install writes them, doctor
// compares them, uninstall removes them.

const (
	engineUnit  = "media-core-engine.service"
	aria2Unit   = "media-core-aria2.service"
	backupUnit  = "media-core-backup.service"
	backupTimer = "media-core-backup.timer"
)

// unitNames: every unit this binary may write, in the order they are written.
var unitNames = []string{aria2Unit, engineUnit, backupUnit, backupTimer}

const unitHeader = "# Written by media-core-engine install. Edits are replaced on upgrade; use a drop-in instead.\n"

// unitData fills the templates. Paths are absolute: the real aria2c is the point, and folders that aren't
// the default ones then work too.
type unitData struct {
	Bin, Aria2c, ConfDir, DataDir, Path string
	DefaultDirs                         bool // false adds Environment=MEDIA_CORE_CONFIG_DIR/DATA_DIR lines
}

var unitTemplates = map[string]string{
	aria2Unit: `[Unit]
Description=Media Core downloader (aria2, RPC on localhost only)

[Service]
ExecStart={{q .Aria2c}} {{q (print "--conf-path=" .ConfDir "/aria2.conf")}}
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
	engineUnit: `[Unit]
Description=Media Core engine: downloads into the library and serves the site and the apps
{{if .Aria2c}}After=network-online.target media-core-aria2.service
Wants=media-core-aria2.service
{{else}}After=network-online.target
{{end}}
[Service]
ExecStart={{q .Bin}} serve
Restart=always
RestartSec=3
Environment={{q (print "PATH=" .Path)}}
{{template "dirs" .}}UMask=0002
# post-processing (ffmpeg, 7z) picks up again after a restart, so a quick stop is fine
TimeoutStopSec=10
# a stream keeps up to about 150M of pieces in memory while a title plays (stream.go)
MemoryHigh=800M

[Install]
WantedBy=default.target
`,
	backupUnit: `[Unit]
Description=Media Core: back up watch progress, settings and devices

[Service]
Type=oneshot
ExecStart={{q .Bin}} backup
{{template "dirs" .}}Nice=19
IOSchedulingClass=idle
`,
	backupTimer: `[Unit]
Description=Media Core: nightly backup

[Timer]
OnCalendar=*-*-* 04:00:00
RandomizedDelaySec=15m
Persistent=true

[Install]
WantedBy=timers.target
`,
}

const unitDirsTemplate = `{{define "dirs"}}{{if not .DefaultDirs}}Environment={{q (print "MEDIA_CORE_CONFIG_DIR=" .ConfDir)}}
Environment={{q (print "MEDIA_CORE_DATA_DIR=" .DataDir)}}
{{end}}{{end}}`

// unitQuote writes s as one word in a unit file: % doubled (systemd reads it as a specifier), and quoted
// when it holds a space or a quote.
func unitQuote(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func unitDir() string { return filepath.Join(homeDir(), ".config", "systemd", "user") }

// currentUnitData: what this computer's units should hold, for the binary at bin.
func currentUnitData(bin string) unitData {
	d := unitData{Bin: bin, ConfDir: ConfDir, DataDir: DataDir,
		DefaultDirs: ConfDir == filepath.Join(homeDir(), ".config/media-core") && DataDir == filepath.Join(homeDir(), ".local/share/media-core")}
	if p, err := exec.LookPath("aria2c"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			d.Aria2c = abs
		}
	}
	dirs := []string{filepath.Dir(bin)}
	for _, t := range tools {
		if p := toolPath(t.ID); p != "" {
			if abs, err := filepath.Abs(p); err == nil {
				dirs = append(dirs, filepath.Dir(abs))
			}
		}
	}
	d.Path = strings.Join(uniq(append(dirs, "/usr/local/bin", "/usr/bin", "/bin")), ":")
	return d
}

// renderUnits: each unit's name and text. There's no aria2 unit when aria2c is missing; the engine then runs
// aria2 itself once it is installed (startAria2).
func renderUnits(d unitData) map[string]string {
	out := map[string]string{}
	for _, name := range unitNames {
		if name == aria2Unit && d.Aria2c == "" {
			continue
		}
		t := template.Must(template.New(name).Funcs(template.FuncMap{"q": unitQuote}).Parse(unitDirsTemplate + unitTemplates[name]))
		var b bytes.Buffer
		if err := t.Execute(&b, d); err != nil {
			panic(err) // the templates are fixed text: a failure is a bug in them
		}
		out[name] = unitHeader + b.String()
	}
	return out
}

// writeUnits writes the units into dir and lists the ones whose text changed.
func writeUnits(dir, bin string) (changed []string, err error) {
	return writeUnitsFrom(dir, currentUnitData(bin))
}

func writeUnitsFrom(dir string, d unitData) (changed []string, err error) {
	units := renderUnits(d)
	for _, name := range unitNames {
		text, ok := units[name]
		if !ok {
			continue
		}
		did, err := writeIfChanged(filepath.Join(dir, name), text, 0o644)
		if err != nil {
			return changed, err
		}
		if did {
			changed = append(changed, name)
		}
	}
	return changed, nil
}

// writeIfChanged writes text to path when it differs (through a temp file), and says whether it did.
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
