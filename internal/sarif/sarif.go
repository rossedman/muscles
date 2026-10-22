// Package sarif turns an images check report into a SARIF 2.1.0 log, the
// format code scanning tools (GitHub code scanning, GitLab, Azure DevOps,
// editor viewers) read. Failures become errors and warnings stay warnings;
// rules that passed or were skipped are left out, since SARIF results are
// findings. It does no I/O: the caller says where each finding lives.
package sarif

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/rossedman/muscles/internal/check"
)

// Schema and Version name the SARIF format written.
const (
	Schema  = "https://json.schemastore.org/sarif-2.1.0.json"
	Version = "2.1.0"
)

// docsURL is where each rule's help page lives.
const docsURL = "https://rossedman.io/muscles/"

// Log is a SARIF log with one run.
type Log struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []Run  `json:"runs"`
}

// Run is one tool's results.
type Run struct {
	Tool        Tool         `json:"tool"`
	Results     []Result     `json:"results"`
	Invocations []Invocation `json:"invocations"`
}

// Tool describes muscles and the rules it reports.
type Tool struct {
	Driver Driver `json:"driver"`
}

// Driver is the tool component that ran.
type Driver struct {
	Name           string `json:"name"`
	Version        string `json:"version,omitempty"`
	InformationURI string `json:"informationUri"`
	Rules          []Rule `json:"rules"`
}

// Rule describes one kind of finding.
type Rule struct {
	ID                   string         `json:"id"`
	Name                 string         `json:"name"`
	ShortDescription     Text           `json:"shortDescription"`
	FullDescription      Text           `json:"fullDescription"`
	HelpURI              string         `json:"helpUri"`
	Help                 Text           `json:"help"`
	DefaultConfiguration Config         `json:"defaultConfiguration"`
	Properties           map[string]any `json:"properties,omitempty"`
}

// Text is a SARIF message string.
type Text struct {
	Text string `json:"text"`
}

// Config is a rule's default severity.
type Config struct {
	Level string `json:"level"`
}

// Result is one finding.
type Result struct {
	RuleID              string            `json:"ruleId"`
	RuleIndex           int               `json:"ruleIndex"`
	Level               string            `json:"level"`
	Message             Text              `json:"message"`
	Locations           []Location        `json:"locations,omitempty"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
	Properties          map[string]any    `json:"properties,omitempty"`
}

// Location is where a finding is, in a file of the repository.
type Location struct {
	PhysicalLocation PhysicalLocation `json:"physicalLocation"`
}

// PhysicalLocation is a file and a line in it.
type PhysicalLocation struct {
	ArtifactLocation ArtifactLocation `json:"artifactLocation"`
	Region           *Region          `json:"region,omitempty"`
}

// ArtifactLocation is a file, relative to the repository root.
type ArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId"`
}

// Region is a line in a file.
type Region struct {
	StartLine int `json:"startLine"`
}

// Invocation says whether the run completed; a failed check still did.
type Invocation struct {
	ExecutionSuccessful bool `json:"executionSuccessful"`
}

// Where is a file in the repository (slash-separated, relative to its root)
// and a line in it; Line 0 means the whole file, and an empty File means
// nowhere.
type Where struct {
	File string
	Line int
}

// Locate says where a finding lives, for example the Dockerfile line that
// made the layer at fault, or the line of .muscles.yaml that set a budget.
type Locate func(check.Result) Where

// ruleInfo is what the log says about each rule muscles can report.
type ruleInfo struct {
	short, full, help string
	level             string // default level
	page              string // docs page and anchor
}

// rules describes every rule images check reports, by its name there.
var rules = map[string]ruleInfo{
	"size":               {"Image too big", "The uncompressed image is bigger than check.max_size.", "Run `muscles images inspect IMAGE` for the fixes that save the most.", "error", "checks/#the-rules"},
	"compressed":         {"Image too big to pull", "The compressed image is bigger than check.max_compressed.", "Run `muscles images inspect IMAGE` for the fixes that save the most.", "error", "checks/#the-rules"},
	"waste":              {"Too much waste", "Layers store more bytes that the final image never shows than check.max_waste allows.", "Run `muscles images inspect IMAGE` for the fixes that save the most.", "error", "waste/"},
	"growth":             {"Image grew too much", "The image grew more than check.max_growth compared with the baseline.", "Run `muscles images diff BASE IMAGE` to see what grew.", "error", "diff/"},
	"layers":             {"Too many layers", "The image has more layers than check.max_layers.", "Combine consecutive RUN steps, or build in stages and copy only the result.", "error", "checks/#the-rules"},
	"forbid":             {"Forbidden path in image", "A path in the final image matches a check.forbid pattern and no check.allow pattern.", "Keep it out of the build context with .dockerignore, or stop adding it.", "error", "checks/#patterns"},
	"package cache":      {"Package cache left in layer", "A package manager's cache is stored in the layer that filled it.", "Clean the cache in the same RUN that fills it, or use a cache mount.", "warning", "layers/#notes"},
	"hidden delete":      {"Deletion frees nothing", "A layer deletes files an earlier layer still stores, so the image doesn't get smaller.", "Delete the files in the step that creates them, or build in an earlier stage and copy only what you need.", "warning", "layers/#notes"},
	"permission rewrite": {"Files stored again for owner or mode", "A layer stores files again only to change owner or permissions; both copies are kept.", "Set the owner as the files are added (COPY --chown) and drop the chown -R.", "warning", "layers/#notes"},
	"overwrite":          {"Earlier files replaced", "A layer replaces files earlier layers still store; both copies are kept.", "Install the final version once, or start from a base image that already has it.", "warning", "layers/#notes"},
	"large file":         {"Large file", "A single file is at least warn.large_file.", "If it isn't needed at run time, keep it out with .dockerignore or a multi-stage build.", "warning", "layers/#notes"},
}

// ruleID is a rule's SARIF id: "package cache" is "muscles/package-cache".
func ruleID(rule string) string { return "muscles/" + strings.ReplaceAll(rule, " ", "-") }

// camel is a rule's SARIF name: "package cache" is "PackageCache".
func camel(rule string) string {
	var b strings.Builder
	for _, w := range strings.Fields(rule) {
		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	return b.String()
}

// level maps a check status to a SARIF level, or "" for none.
func level(s check.Status) string {
	switch s {
	case check.Fail:
		return "error"
	case check.Warn:
		return "warning"
	}
	return ""
}

// fingerprint identifies a finding across builds: its rule and its key,
// never its sizes or the image's name.
func fingerprint(r check.Result) string {
	sum := sha256.Sum256([]byte(r.Rule + "\x00" + r.Key))
	return hex.EncodeToString(sum[:16])
}

// Build makes the SARIF log for rep. version is muscles' version; locate
// says where each finding lives.
func Build(rep check.Report, version string, locate Locate) Log {
	results := []Result{}
	var ruleOf []string // each result's rule
	index := map[string]int{}
	var used []string
	for _, r := range rep.Results {
		lvl := level(r.Status)
		if lvl == "" {
			continue
		}
		if _, ok := index[r.Rule]; !ok {
			index[r.Rule] = 0 // set once the rules are sorted
			used = append(used, r.Rule)
		}
		msg := r.Message
		if r.Detail != "" {
			msg += " (" + r.Detail + ")"
		}
		if r.Fix != "" {
			msg += ". Fix: " + r.Fix
		}
		res := Result{RuleID: ruleID(r.Rule), Level: lvl, Message: Text{msg},
			PartialFingerprints: map[string]string{"muscles/v1": fingerprint(r)},
			Properties:          map[string]any{"image": rep.Image}}
		if r.Layer != nil {
			res.Properties["layer"] = *r.Layer
		}
		if w := locate(r); w.File != "" {
			pl := PhysicalLocation{ArtifactLocation: ArtifactLocation{URI: w.File, URIBaseID: "%SRCROOT%"}}
			if w.Line > 0 {
				pl.Region = &Region{StartLine: w.Line}
			}
			res.Locations = []Location{{PhysicalLocation: pl}}
		}
		results = append(results, res)
		ruleOf = append(ruleOf, r.Rule)
	}

	sort.Strings(used)
	driver := Driver{Name: "muscles", Version: version, InformationURI: docsURL, Rules: []Rule{}}
	for i, name := range used {
		index[name] = i
		info, ok := rules[name]
		if !ok {
			info = ruleInfo{short: name, full: name, help: "", level: "warning", page: "checks/"}
		}
		driver.Rules = append(driver.Rules, Rule{
			ID: ruleID(name), Name: camel(name),
			ShortDescription: Text{info.short}, FullDescription: Text{info.full},
			HelpURI: docsURL + info.page, Help: Text{info.help},
			DefaultConfiguration: Config{Level: info.level},
			Properties:           map[string]any{"tags": []string{"container", "image-size"}},
		})
	}
	for i := range results {
		results[i].RuleIndex = index[ruleOf[i]]
	}
	return Log{Schema: Schema, Version: Version, Runs: []Run{{
		Tool:        Tool{Driver: driver},
		Results:     results,
		Invocations: []Invocation{{ExecutionSuccessful: true}},
	}}}
}
