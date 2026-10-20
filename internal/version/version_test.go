package version

import (
	"runtime/debug"
	"testing"
)

func TestGet(t *testing.T) {
	defer func(v, c string, r func() (*debug.BuildInfo, bool)) { Version, Commit, readBuildInfo = v, c, r }(Version, Commit, readBuildInfo)

	Version, Commit = "v1.2.3", "abc"
	if got := Get(); got.Version != "v1.2.3" || got.Commit != "abc" || got.Platform == "" || got.Go == "" {
		t.Fatalf("ldflags: %+v", got)
	}

	Version, Commit = "", ""
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main:     debug.Module{Version: "v0.9.0"},
			Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "0123456789abcdef"}},
		}, true
	}
	if got := Get(); got.Version != "v0.9.0" || got.Commit != "0123456789ab" {
		t.Fatalf("build info: %+v", got)
	}

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261020154433-76096f07c8c3+dirty"}}, true
	}
	if got := Get(); got.Version != "dev" || got.Commit != "" {
		t.Fatalf("devel: %+v", got)
	}
}
