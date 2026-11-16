package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvFileAndEnvOrFrom(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "engine.env")
	content := strings.Join([]string{
		"# comment line",
		"   # indented comment",
		"",
		"   ",
		`MEDIA_CORE_NAME="Living Room"`,
		`MEDIA_CORE_LIBRARY='/mnt/media/'`,
		`MEDIA_CORE_PUBLIC_HOST=media.example.com`,
		`MEDIA_CORE_LISTEN=:9780`,
		`EMPTY_VAL=""`,
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got := parseEnvFile(path)
	if got["MEDIA_CORE_NAME"] != "Living Room" {
		t.Fatalf("double-quoted value = %q, want %q", got["MEDIA_CORE_NAME"], "Living Room")
	}
	if got["MEDIA_CORE_LIBRARY"] != "/mnt/media/" {
		t.Fatalf("single-quoted value = %q, want %q", got["MEDIA_CORE_LIBRARY"], "/mnt/media/")
	}
	if got["MEDIA_CORE_PUBLIC_HOST"] != "media.example.com" {
		t.Fatalf("unquoted value = %q, want %q", got["MEDIA_CORE_PUBLIC_HOST"], "media.example.com")
	}

	// File supplies value when env var is unset.
	t.Setenv("MEDIA_CORE_NAME", "")
	if v := envOrFrom("MEDIA_CORE_NAME", "Media Core", path); v != "Living Room" {
		t.Fatalf("envOrFrom from file = %q, want %q", v, "Living Room")
	}
	// Trailing slash stripped just like os.Getenv path.
	t.Setenv("MEDIA_CORE_LIBRARY", "")
	if v := envOrFrom("MEDIA_CORE_LIBRARY", "/srv/data/media", path); v != "/mnt/media" {
		t.Fatalf("envOrFrom trimmed = %q, want %q", v, "/mnt/media")
	}
	// Real environment wins over file.
	t.Setenv("MEDIA_CORE_NAME", "Env Box")
	if v := envOrFrom("MEDIA_CORE_NAME", "Media Core", path); v != "Env Box" {
		t.Fatalf("envOrFrom env override = %q, want %q", v, "Env Box")
	}
	// Empty value or missing key falls back to default.
	t.Setenv("EMPTY_VAL", "")
	if v := envOrFrom("EMPTY_VAL", "fallback", path); v != "fallback" {
		t.Fatalf("envOrFrom empty = %q, want %q", v, "fallback")
	}
	if v := envOrFrom("MISSING_KEY", "def", filepath.Join(dir, "nonexistent.env")); v != "def" {
		t.Fatalf("envOrFrom missing file = %q, want %q", v, "def")
	}
}

func TestConfigDirs(t *testing.T) {
	homeDir := "/tmp/test-home"
	t.Setenv("HOME", homeDir)
	t.Setenv("MEDIA_CORE_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("MEDIA_CORE_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")

	if got := defaultConfDir(); got != filepath.Join(homeDir, ".config/media-core") {
		t.Fatalf("default ConfDir = %q", got)
	}
	if got := defaultDataDir(); got != filepath.Join(homeDir, ".local/share/media-core") {
		t.Fatalf("default DataDir = %q", got)
	}

	// XDG_* is ignored on purpose: the aria2 unit and backup script use the fixed paths
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-cfg/")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data/")
	if got := defaultConfDir(); got != filepath.Join(homeDir, ".config/media-core") {
		t.Fatalf("ConfDir with XDG set = %q", got)
	}
	if got := defaultDataDir(); got != filepath.Join(homeDir, ".local/share/media-core") {
		t.Fatalf("DataDir with XDG set = %q", got)
	}

	t.Setenv("MEDIA_CORE_CONFIG_DIR", "/tmp/explicit-cfg/")
	t.Setenv("MEDIA_CORE_DATA_DIR", "/tmp/explicit-data/")
	if got := defaultConfDir(); got != "/tmp/explicit-cfg" {
		t.Fatalf("explicit ConfDir = %q", got)
	}
	if got := defaultDataDir(); got != "/tmp/explicit-data" {
		t.Fatalf("explicit DataDir = %q", got)
	}
}

func TestServerNameAndDoviToolAndAddrs(t *testing.T) {
	t.Setenv("MEDIA_CORE_NAME", "Cinema Box")
	if got := serverName(); got != "Cinema Box" {
		t.Fatalf("serverName() = %q, want %q", got, "Cinema Box")
	}
	t.Setenv("MEDIA_CORE_NAME", "")
	if got := serverName(); got == "" || strings.Contains(got, ".") {
		t.Fatalf("default serverName() = %q, want non-empty short name", got)
	}

	// DoviTool default: finds on PATH if present, else $HOME/.local/bin/dovi_tool.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("PATH", t.TempDir())
	if got := defaultDoviTool(); got != filepath.Join(fakeHome, ".local/bin/dovi_tool") {
		t.Fatalf("defaultDoviTool fallback = %q", got)
	}
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "dovi_tool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	if got := defaultDoviTool(); got != bin {
		t.Fatalf("defaultDoviTool on PATH = %q, want %q", got, bin)
	}

	// Listen/player address helpers.
	t.Setenv("MEDIA_CORE_LISTEN", "100.64.0.1:9780")
	if got := adminURL(); got != "http://127.0.0.1:9780" {
		t.Fatalf("adminURL() = %q, want http://127.0.0.1:9780", got)
	}
	t.Setenv("MEDIA_CORE_PLAYER_LISTEN", ":9790")
	if got := playerAddr(); got != ":9790" {
		t.Fatalf("playerAddr() = %q, want :9790", got)
	}
}

func TestTrimSlashAndDiscoverAddr(t *testing.T) {
	for in, want := range map[string]string{"": "", "/": "/", "//": "/", "/srv/media/": "/srv/media", "x": "x"} {
		if got := trimSlash(in); got != want {
			t.Errorf("trimSlash(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{":8790": ":8790", "127.0.0.1:9000": "127.0.0.1:9000", "bad": ":8790"} {
		if got := discoverAddr(in); got != want {
			t.Errorf("discoverAddr(%q) = %q, want %q", in, got, want)
		}
	}
}
