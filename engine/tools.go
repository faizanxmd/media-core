package main

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// The outside programs the engine uses. None is bundled: each one missing turns off only its feature,
// and the setup page, doctor and error messages give the one command that installs it.

type tool struct {
	ID      string            `json:"id"`      // the command name
	Feature string            `json:"feature"` // what stops without it
	Needed  bool              `json:"needed"`  // the server is little use without it
	Pkg     map[string]string `json:"-"`       // package name per package manager ("" = not packaged there)
	Manual  string            `json:"-"`       // where to get it when no package manager has it
}

var tools = []tool{
	{ID: "ffmpeg", Feature: "Playing in browsers, scrub pictures, intros and subtitles", Needed: true,
		Pkg: map[string]string{"apt": "ffmpeg", "dnf": "ffmpeg", "pacman": "ffmpeg", "zypper": "ffmpeg", "brew": "ffmpeg"}},
	{ID: "ffprobe", Feature: "Reading what's in a file", Needed: true,
		Pkg: map[string]string{"apt": "ffmpeg", "dnf": "ffmpeg", "pacman": "ffmpeg", "zypper": "ffmpeg", "brew": "ffmpeg"}},
	{ID: "aria2c", Feature: "Downloading links",
		Pkg: map[string]string{"apt": "aria2", "dnf": "aria2", "pacman": "aria2", "zypper": "aria2", "brew": "aria2"}},
	{ID: "7z", Feature: "Unpacking zip and rar downloads",
		Pkg: map[string]string{"apt": "p7zip-full", "dnf": "p7zip p7zip-plugins", "pacman": "7zip", "zypper": "7zip", "brew": "p7zip"}},
	{ID: "mkvmerge", Feature: "Fixing Dolby Vision profile 7 for Android TVs",
		Pkg: map[string]string{"apt": "mkvtoolnix", "dnf": "mkvtoolnix", "pacman": "mkvtoolnix-cli", "zypper": "mkvtoolnix", "brew": "mkvtoolnix"}},
	{ID: "dovi_tool", Feature: "Fixing Dolby Vision profile 7 for Android TVs",
		Manual: "download it from github.com/quietvoid/dovi_tool/releases and put it in ~/.local/bin"},
}

var pkgInstall = map[string]string{
	"apt": "sudo apt install", "dnf": "sudo dnf install", "pacman": "sudo pacman -S",
	"zypper": "sudo zypper install", "brew": "brew install",
}

// pkgManager names this machine's package manager, from /etc/os-release (or brew on a Mac).
var pkgManager = sync.OnceValue(func() string {
	if runtime.GOOS == "darwin" {
		return "brew"
	}
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()
	ids := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), "=")
		if k == "ID" || k == "ID_LIKE" {
			ids += " " + strings.Trim(v, `"`)
		}
	}
	for _, w := range strings.Fields(ids) {
		switch w {
		case "debian", "ubuntu", "linuxmint", "pop", "raspbian":
			return "apt"
		case "fedora", "rhel", "centos", "rocky", "almalinux":
			return "dnf"
		case "arch", "manjaro", "endeavouros":
			return "pacman"
		case "opensuse", "suse", "opensuse-leap", "opensuse-tumbleweed":
			return "zypper"
		}
	}
	return ""
})

func toolByID(id string) *tool {
	for i := range tools {
		if tools[i].ID == id {
			return &tools[i]
		}
	}
	return nil
}

// toolPath finds a tool: dovi_tool where the settings say, the rest on the PATH. "" if missing.
func toolPath(id string) string {
	name := id
	if id == "dovi_tool" {
		name = DoviTool
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// installHint is the command that installs a tool here, or where to get it.
func installHint(id string) string {
	t := toolByID(id)
	if t == nil {
		return "install " + id
	}
	if pm := pkgManager(); pm != "" && t.Pkg[pm] != "" {
		return pkgInstall[pm] + " " + t.Pkg[pm]
	}
	if t.Manual != "" {
		return t.Manual
	}
	return "install " + id + " with your package manager"
}

// needTool returns an error that says what stopped and how to fix it, if a tool is missing.
func needTool(what, id string) error {
	if toolPath(id) != "" {
		return nil
	}
	return &toolMissing{what: what, id: id}
}

type toolMissing struct{ what, id string }

func (e *toolMissing) Error() string {
	return e.what + " needs " + e.id + ". On the server: " + installHint(e.id)
}

type toolState struct {
	ID      string `json:"id"`
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Feature string `json:"feature"`
	Needed  bool   `json:"needed"`
	Install string `json:"install,omitempty"` // the command, when missing
	Note    string `json:"note,omitempty"`
}

// chromaprint: ffmpeg needs it to find intros. Asked once per ffmpeg path.
var (
	chromaMu   sync.Mutex
	chromaSeen = map[string]bool{}
)

func hasChromaprint(ffmpeg string) bool {
	chromaMu.Lock()
	defer chromaMu.Unlock()
	if v, ok := chromaSeen[ffmpeg]; ok {
		return v
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-muxers").Output()
	v := strings.Contains(string(out), "chromaprint")
	chromaSeen[ffmpeg] = v
	return v
}

func toolStates() []toolState {
	out := make([]toolState, 0, len(tools))
	for _, t := range tools {
		s := toolState{ID: t.ID, Feature: t.Feature, Needed: t.Needed, Path: toolPath(t.ID)}
		s.Found = s.Path != ""
		if !s.Found {
			s.Install = installHint(t.ID)
		} else if t.ID == "ffmpeg" && !hasChromaprint(s.Path) {
			s.Note = "This ffmpeg has no chromaprint, so Skip Intro is off"
		}
		out = append(out, s)
	}
	return out
}

func serveTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"tools": toolStates(), "package_manager": pkgManager()})
}
