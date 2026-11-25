package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// version is set by the release build:
//
//	go build -ldflags "-X main.version=v0.1.0" ./cmd/x3f
//
// -X overwrites the value of a package-level string variable at link time.
var version = ""

// buildVersion returns the version of this binary: the one set at build
// time, else the module version the Go toolchain recorded ("v0.1.0" for
// "go install ...@v0.1.0", a pseudo-version with the commit for a build in
// a git checkout), else "dev".
func buildVersion() string {
	if version != "" {
		return version
	}
	// ReadBuildInfo returns what the Go toolchain embeds in every binary:
	// the module version and, for builds in a git checkout, the commit.
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// buildCommit returns the short git commit the binary was built from, or
// "unknown".
func buildCommit() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return s.Value[:7]
			}
		}
	}
	return "unknown"
}

// versionText is the output of -version, e.g.
// "x3f v0.1.0 (commit 4d9651e, go1.27.4, linux/amd64)".
func versionText() string {
	return fmt.Sprintf("x3f %s (commit %s, %s, %s/%s)", buildVersion(), buildCommit(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
