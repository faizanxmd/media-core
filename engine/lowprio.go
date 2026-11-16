package main

import "os/exec"

// prioPrefix is what runs a heavy tool at low priority: ionice (Linux) around nice, or nice alone where
// there is no ionice (macOS, minimal containers), or nothing at all.
// idle: the lowest class (thumbnails); otherwise best-effort, lowest level (post-processing).
func prioPrefix(idle bool, niceness string) []string {
	var out []string
	if _, err := exec.LookPath("ionice"); err == nil {
		if idle {
			out = append(out, "ionice", "-c3")
		} else {
			out = append(out, "ionice", "-c2", "-n7")
		}
	}
	if _, err := exec.LookPath("nice"); err == nil {
		out = append(out, "nice", "-n", niceness)
	}
	return out
}

// lowPrioArgv returns the program and arguments that run name at low priority.
func lowPrioArgv(idle bool, niceness, name string, args ...string) (string, []string) {
	argv := append(prioPrefix(idle, niceness), name)
	argv = append(argv, args...)
	return argv[0], argv[1:]
}
