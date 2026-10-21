package source

import (
	"strings"
	"testing"
)

// FuzzParsePlatform feeds arbitrary --platform values to ParsePlatform: a
// value is accepted only with an OS and an architecture, and an accepted
// platform written back as os/arch[/variant] parses to the same platform.
func FuzzParsePlatform(f *testing.F) {
	for _, s := range []string{"", "linux", "linux/amd64", "linux/arm64/v8", "linux/arm/v7", "windows/amd64:10.0.17763.1234", "linux/amd64/v3/extra", "/amd64", "linux/", " linux/arm64 ", "a:b:c", "linux//v8"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, err := ParsePlatform(s)
		if s == "" {
			if p != nil || err != nil {
				t.Fatalf("ParsePlatform(\"\") = %v, %v; want unset", p, err)
			}
			return
		}
		if err != nil {
			if p != nil {
				t.Fatalf("ParsePlatform(%q) = %v with error %v", s, p, err)
			}
			if !strings.Contains(err.Error(), "os/arch") {
				t.Fatalf("ParsePlatform(%q): error %q doesn't say what to write", s, err)
			}
			return
		}
		if p == nil || p.OS == "" || p.Architecture == "" {
			t.Fatalf("ParsePlatform(%q) = %+v, want an OS and an architecture", s, p)
		}
		for _, field := range []string{p.OS, p.Architecture, p.Variant} {
			if strings.ContainsAny(field, "/:") {
				t.Fatalf("ParsePlatform(%q) = %+v: a field holds a separator", s, p)
			}
		}
		formatted := platformString(*p)
		if formatted != strings.TrimSpace(formatted) {
			// The parser trims the whole value, so a field that begins or
			// ends with space can't survive being written back; it can't
			// name a real platform either.
			return
		}
		q, err := ParsePlatform(formatted)
		if err != nil || q.OS != p.OS || q.Architecture != p.Architecture || q.Variant != p.Variant {
			t.Fatalf("ParsePlatform(%q) = %+v, but %q parses to %+v, %v", s, p, formatted, q, err)
		}
	})
}

// BenchmarkParsePlatform measures parsing the --platform values people use.
func BenchmarkParsePlatform(b *testing.B) {
	ins := []string{"linux/amd64", "linux/arm64/v8", "windows/amd64:10.0.17763.1234"}
	b.ReportAllocs()
	for b.Loop() {
		for _, s := range ins {
			if _, err := ParsePlatform(s); err != nil {
				b.Fatal(err)
			}
		}
	}
}
