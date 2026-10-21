package size

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// sizeSeeds are the sizes users write in flags, config and the Action.
var sizeSeeds = []string{"0", "512", "512B", "1KB", "1.5 GB", "800MB", "2KiB", "1GiB", "1TiB", "3tb", "4k", " 10 mb ", "", "MB", "12XB", "-5MB", "1.2.3MB", "10%", " 2.5 % ", "1e3MB", "-0"}

// FuzzParseBytes checks that any accepted byte count is non-negative (an
// overflowing one wraps negative on amd64), that a byte count too large for
// an int64 is refused rather than saturated, and that a plain integer parses
// to itself.
func FuzzParseBytes(f *testing.F) {
	for _, s := range sizeSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := ParseBytes(s)
		if err != nil {
			if n != 0 {
				t.Fatalf("ParseBytes(%q) = %d with error %v", s, n, err)
			}
			return
		}
		if n < 0 {
			t.Fatalf("ParseBytes(%q) = %d: out of range, want an error", s, n)
		}
		if n == math.MaxInt64 {
			// Only a size that rounds to exactly 2^63 may land here; ten
			// times it must be refused.
			if _, err := ParseBytes(s + "0"); err == nil && !strings.ContainsAny(s, ".eE") {
				t.Fatalf("ParseBytes(%q0) succeeded past the largest size", s)
			}
		}
		if i, err := strconv.ParseInt(s, 10, 64); err == nil && i >= 0 && i <= 1<<53 && n != i {
			t.Fatalf("ParseBytes(%q) = %d, want %d", s, n, i)
		}
	})
}

// FuzzParseLimit checks that an accepted limit is a finite, non-negative
// percentage or byte count, never both, and that it survives being written
// back the way a user would.
func FuzzParseLimit(f *testing.F) {
	for _, s := range sizeSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		l, err := ParseLimit(s)
		if err != nil {
			if !l.IsZero() {
				t.Fatalf("ParseLimit(%q) = %+v with error %v", s, l, err)
			}
			return
		}
		if l.Bytes < 0 || l.Percent < 0 || math.IsNaN(l.Percent) || math.IsInf(l.Percent, 0) {
			t.Fatalf("ParseLimit(%q) = %+v: not a finite, non-negative limit", s, l)
		}
		if l.Bytes != 0 && l.Percent != 0 {
			t.Fatalf("ParseLimit(%q) = %+v: both bytes and percent", s, l)
		}
		if l.Percent != 0 {
			back, err := ParseLimit(l.String())
			if err != nil || back != l {
				t.Fatalf("ParseLimit(%q) = %+v, but its String %q parses to %+v, %v", s, l, l.String(), back, err)
			}
		}
	})
}

// FuzzFormatParse checks that what Format prints, ParseBytes reads back to
// within Format's rounding: one decimal under 10 units, whole units above,
// so never more than 5% off.
func FuzzFormatParse(f *testing.F) {
	for _, n := range []int64{0, 1, 999, 1000, 1023, 1024, 9_999, 7_300_000, 182_000_000, 1 << 40, math.MaxInt64} {
		f.Add(n, false)
		f.Add(n, true)
	}
	f.Fuzz(func(t *testing.T, n int64, iec bool) {
		if n < 0 {
			n = -(n + 1)
		}
		s := Format(n, iec)
		got, err := ParseBytes(s)
		if err != nil {
			t.Fatalf("ParseBytes(Format(%d)) = %q: %v", n, s, err)
		}
		if d := math.Abs(float64(got) - float64(n)); d > float64(n)/20+1 {
			t.Fatalf("ParseBytes(Format(%d, %v)) = ParseBytes(%q) = %d, off by %.0f", n, iec, s, got, d)
		}
		if n < 1000 && got != n {
			t.Fatalf("ParseBytes(Format(%d)) = %d: small sizes are exact", n, got)
		}
	})
}

// BenchmarkParseBytes measures parsing the sizes a budget file holds.
func BenchmarkParseBytes(b *testing.B) {
	ins := []string{"800MB", "1.5GiB", " 10 mb ", "512"}
	b.ReportAllocs()
	for b.Loop() {
		for _, s := range ins {
			if _, err := ParseBytes(s); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkParseLimit measures parsing byte and percentage limits.
func BenchmarkParseLimit(b *testing.B) {
	ins := []string{"50MB", "10%", "1.5GiB", ""}
	b.ReportAllocs()
	for b.Loop() {
		for _, s := range ins {
			if _, err := ParseLimit(s); err != nil {
				b.Fatal(err)
			}
		}
	}
}
