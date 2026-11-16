package main

import "runtime/debug"

// version is set at release time: go build -ldflags "-X main.version=1.2.3". A plain build says "dev".
var version = "dev"

// A build from source without a release version says which commit it is: "dev+abc1234".
func init() {
	if version != "dev" {
		return
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				version = "dev+" + s.Value[:7]
			}
		}
	}
}
