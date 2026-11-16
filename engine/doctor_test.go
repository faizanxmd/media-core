package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorRollUp(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{{nil, "ok"}, {[]string{"ok", "warn"}, "warn"}, {[]string{"warn", "fail", "ok"}, "fail"}, {[]string{"ok"}, "ok"}} {
		var cs []check
		for _, s := range tc.in {
			cs = append(cs, check{Status: s})
		}
		if got := rollUp(cs); got != tc.want {
			t.Fatalf("%v: %s, want %s", tc.in, got, tc.want)
		}
	}
}

// The check ids are a public contract: one listed in testdata/doctor-ids.txt must never disappear.
func TestDoctorIDsStay(t *testing.T) {
	want, err := os.ReadFile("testdata/doctor-ids.txt")
	if err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile("doctor.go")
	for _, id := range strings.Fields(string(want)) {
		if !strings.Contains(string(src), `"`+id) {
			t.Errorf("doctor no longer has the check %q (ids are public: add, never rename)", id)
		}
	}
}

func TestDoctorReport(t *testing.T) {
	fakeHome(t)
	stubSystem(t, filepath.Join(t.TempDir(), "media-core-engine"))
	noServer(t)
	rep := runDoctor(false)
	b, _ := json.Marshal(rep)
	var shape map[string]any
	json.Unmarshal(b, &shape)
	for _, k := range []string{"schema", "version", "os", "arch", "config_dir", "data_dir", "status", "checks"} {
		if _, ok := shape[k]; !ok {
			t.Errorf("the report has no %q", k)
		}
	}
	seen := map[string]bool{}
	for _, c := range rep.Checks {
		if seen[c.ID] {
			t.Errorf("check %q appears twice", c.ID)
		}
		seen[c.ID] = true
		if c.Status != "ok" && c.Status != "warn" && c.Status != "fail" {
			t.Errorf("%s: status %q", c.ID, c.Status)
		}
		if c.Status != "ok" && c.Fix == "" {
			t.Errorf("%s is %s with no fix", c.ID, c.Status)
		}
		if c.ID == "update" {
			t.Error("doctor went online without --online")
		}
	}
	if rep.Status != rollUp(rep.Checks) {
		t.Fatal("status doesn't match the checks")
	}
	if !seen["server"] || !seen["library"] || rep.Status != "fail" {
		t.Fatalf("a fresh home with no server: %+v", rep)
	}
}
