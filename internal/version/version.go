// Package version reports the build's version.
package version

import (
	"regexp"
	"runtime"
	"runtime/debug"
)

// Set at build time:
//
//	go build -ldflags "-X github.com/rossedman/muscles/internal/version.Version=v0.1.0 -X github.com/rossedman/muscles/internal/version.Commit=abc123"
var (
	Version = ""
	Commit  = ""
)

// Info describes this binary.
type Info struct {
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"`
	Go       string `json:"go"`
	Platform string `json:"platform"`
}

// pseudo matches Go's pseudo-versions (v0.0.0-20261020154433-76096f07c8c3),
// which name a commit, not a release.
var pseudo = regexp.MustCompile(`^v\d+\.\d+\.\d+-(0\.)?\d{14}-[0-9a-f]{12}`)

// readBuildInfo is replaceable so tests can cover the fallbacks.
var readBuildInfo = debug.ReadBuildInfo

// Get returns build information, falling back to the module data that `go
// install` records when no ldflags were set.
func Get() Info {
	info := Info{Version: Version, Commit: Commit, Go: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH}
	if bi, ok := readBuildInfo(); ok {
		if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" && !pseudo.MatchString(bi.Main.Version) {
			info.Version = bi.Main.Version
		}
		if info.Commit == "" {
			for _, s := range bi.Settings {
				if s.Key == "vcs.revision" && len(s.Value) >= 12 {
					info.Commit = s.Value[:12]
				}
			}
		}
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	return info
}
