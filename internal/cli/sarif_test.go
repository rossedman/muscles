package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sarifResults decodes a SARIF log and returns "ruleId file:line" for each
// result, "ruleId -" for one with no location.
func sarifResults(t *testing.T, out string) []string {
	t.Helper()
	var log struct {
		Version string
		Runs    []struct {
			Results []struct {
				RuleID    string `json:"ruleId"`
				Level     string
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct{ URI string }
						Region           *struct{ StartLine int }
					}
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &log); err != nil || log.Version != "2.1.0" {
		t.Fatalf("not SARIF 2.1.0 (%v):\n%s", err, out)
	}
	var got []string
	for _, r := range log.Runs[0].Results {
		where := "-"
		if len(r.Locations) > 0 {
			l := r.Locations[0].PhysicalLocation
			where = l.ArtifactLocation.URI
			if l.Region != nil {
				where += fmt.Sprintf(":%d", l.Region.StartLine)
			}
		}
		got = append(got, r.RuleID+" "+r.Level+" "+where)
	}
	return got
}

func has(t *testing.T, got []string, want ...string) {
	t.Helper()
	all := "\n" + strings.Join(got, "\n") + "\n"
	for _, w := range want {
		if !strings.Contains(all, "\n"+w+"\n") {
			t.Errorf("no %q in:%s", w, all)
		}
	}
}

func TestImagesCheckSARIF(t *testing.T) {
	e := newEnv(t)
	df := e.dockerfile()
	t.Setenv("GITHUB_WORKSPACE", e.dirs.Cwd)
	t.Setenv("GITHUB_ACTIONS", "true")
	e.project("dockerfile: " + df + "\ncheck:\n  max_size: 1KB\n  forbid: [\"**/.env\"]\n")
	r := e.run("images", "check", fixtures.v1, "-o", "sarif")
	must(t, r, 5, nil, []string{"Error: 2 of 2 checks failed"})
	if strings.Contains(r.out, "::error") || strings.Contains(r.out, "::warning") {
		t.Errorf("annotations in SARIF output:\n%s", r.out)
	}
	got := sarifResults(t, r.out)
	has(t, got,
		"muscles/size error .muscles.yaml:3",         // the budget's line
		"muscles/forbid error Dockerfile:7",          // the chown that stored /app/.env last
		"muscles/package-cache warning Dockerfile:3", // the RUN that left the lists
		"muscles/hidden-delete warning Dockerfile:5")
	if len(got) != 4 {
		t.Errorf("passes reported: %v", got)
	}

	// A budget from a flag points at the top of the Dockerfile; with no
	// Dockerfile, the project file; with neither, nowhere.
	e.project("dockerfile: " + df + "\n")
	has(t, sarifResults(t, e.run("images", "check", fixtures.v1, "-o", "sarif", "--max-size", "1KB").out), "muscles/size error Dockerfile:1")
	e.project("output:\n  units: si\n")
	has(t, sarifResults(t, e.run("images", "check", fixtures.v1, "-o", "sarif", "--max-size", "1KB").out), "muscles/size error .muscles.yaml:1", "muscles/package-cache warning .muscles.yaml:1")
	// A config file outside the repository is never a location.
	outside := filepath.Join(t.TempDir(), "muscles.yaml")
	os.WriteFile(outside, []byte("output:\n  units: si\n"), 0o644)
	r = e.run("images", "check", fixtures.v1, "-o", "sarif", "--max-size", "1GB", "--config", outside)
	must(t, r, 0, nil, nil)
	has(t, sarifResults(t, r.out), "muscles/package-cache warning -")

	// SARIF reports findings: other commands refuse it.
	must(t, e.run("layers", "list", fixtures.v1, "-o", "sarif"), 2, nil, []string{"-o sarif is for `muscles images check` only"})
	must(t, e.run("images", "check", fixtures.v1, "-o", "sarf"), 2, nil, []string{"use table, plain, json, markdown or sarif"})
}
