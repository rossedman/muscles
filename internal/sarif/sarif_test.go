package sarif

import (
	"encoding/json"
	"testing"

	"github.com/rossedman/muscles/internal/check"
)

func TestBuild(t *testing.T) {
	two := 2
	rep := check.Report{Image: "app:dev", Results: []check.Result{
		{Rule: "size", Status: check.Fail, Message: "251 MB > 100 MB", Detail: "biggest layer: L3", Fix: "Run inspect."},
		{Rule: "layers", Status: check.Pass, Message: "6 ≤ 40"},
		{Rule: "growth", Status: check.Skip, Message: "no baseline image"},
		{Rule: "package cache", Status: check.Warn, Message: "apt lists left behind: 9 MB", Fix: "End this RUN with rm.", Layer: &two, Line: 3, Key: "RUN apt-get\x00/var/lib/apt/lists"},
		{Rule: "new rule", Status: check.Warn, Message: "something"},
	}}
	locate := func(r check.Result) Where {
		switch r.Rule {
		case "size":
			return Where{File: ".muscles.yaml", Line: 2}
		case "package cache":
			return Where{File: "Dockerfile", Line: r.Line}
		case "new rule":
			return Where{File: "Dockerfile"}
		}
		return Where{}
	}
	log := Build(rep, "v1.2.3", locate)
	if log.Version != "2.1.0" || log.Schema != Schema || len(log.Runs) != 1 {
		t.Fatalf("envelope: %+v", log)
	}
	run := log.Runs[0]
	if !run.Invocations[0].ExecutionSuccessful || run.Tool.Driver.Version != "v1.2.3" {
		t.Errorf("run: %+v", run)
	}
	// Passes and skips are not findings; rules are sorted and indexed.
	if len(run.Results) != 3 {
		t.Fatalf("%d results: %+v", len(run.Results), run.Results)
	}
	rules := run.Tool.Driver.Rules
	if len(rules) != 3 || rules[0].ID != "muscles/new-rule" || rules[1].ID != "muscles/package-cache" || rules[1].Name != "PackageCache" || rules[2].ID != "muscles/size" {
		t.Fatalf("rules: %+v", rules)
	}
	for _, r := range run.Results {
		if rules[r.RuleIndex].ID != r.RuleID {
			t.Errorf("%s indexes rule %d (%s)", r.RuleID, r.RuleIndex, rules[r.RuleIndex].ID)
		}
	}
	if rules[0].DefaultConfiguration.Level != "warning" || rules[2].DefaultConfiguration.Level != "error" || rules[2].HelpURI != docsURL+"checks/#the-rules" {
		t.Errorf("rule info: %+v", rules)
	}

	size, cache, other := run.Results[0], run.Results[1], run.Results[2]
	if size.Level != "error" || size.Message.Text != "251 MB > 100 MB (biggest layer: L3). Fix: Run inspect." {
		t.Errorf("size: %+v", size)
	}
	if l := size.Locations[0].PhysicalLocation; l.ArtifactLocation.URI != ".muscles.yaml" || l.ArtifactLocation.URIBaseID != "%SRCROOT%" || l.Region.StartLine != 2 {
		t.Errorf("size location: %+v", l)
	}
	if cache.Level != "warning" || cache.Properties["layer"] != 2 || cache.Properties["image"] != "app:dev" || cache.Locations[0].PhysicalLocation.Region.StartLine != 3 {
		t.Errorf("cache: %+v", cache)
	}
	if other.Locations[0].PhysicalLocation.Region != nil {
		t.Errorf("a whole-file location has a region: %+v", other.Locations[0])
	}

	// No location: the field is left out.
	b, _ := json.Marshal(Build(check.Report{Results: []check.Result{{Rule: "size", Status: check.Fail}}}, "", func(check.Result) Where { return Where{} }))
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if res := m["runs"].([]any)[0].(map[string]any)["results"].([]any)[0].(map[string]any); res["locations"] != nil {
		t.Errorf("empty location written: %v", res)
	}
	// Nothing to report is still a valid log with empty lists.
	empty := Build(check.Report{}, "", locate)
	if empty.Runs[0].Results == nil || empty.Runs[0].Tool.Driver.Rules == nil {
		t.Error("nil lists")
	}
}

// A finding keeps its fingerprint while its sizes change, so code scanning
// tracks one alert across builds; another path or step is another alert.
func TestFingerprint(t *testing.T) {
	a := check.Result{Rule: "package cache", Message: "apt lists left behind: 9 MB", Key: "RUN apt\x00/var/lib/apt/lists"}
	b := a
	b.Message = "apt lists left behind: 12 MB"
	c := a
	c.Key = "RUN apk\x00/var/cache/apk"
	d := a
	d.Rule = "hidden delete"
	if fingerprint(a) != fingerprint(b) || fingerprint(a) == fingerprint(c) || fingerprint(a) == fingerprint(d) || len(fingerprint(a)) != 32 {
		t.Errorf("fingerprints: %s %s %s %s", fingerprint(a), fingerprint(b), fingerprint(c), fingerprint(d))
	}
}
