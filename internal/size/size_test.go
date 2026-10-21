package size

import (
	"math"
	"testing"
)

func TestParseBytes(t *testing.T) {
	cases := map[string]int64{
		"0":       0,
		"512":     512,
		"512B":    512,
		"1KB":     1000,
		"1.5 GB":  1_500_000_000,
		"800MB":   800_000_000,
		"2KiB":    2048,
		"1MiB":    1 << 20,
		"1GiB":    1 << 30,
		"1TiB":    1 << 40,
		"3tb":     3_000_000_000_000,
		"4k":      4000,
		" 10 mb ": 10_000_000,
		// The largest size: what Format(math.MaxInt64) prints reads back.
		"9223372036854775807": math.MaxInt64,
		"8388608 TiB":         math.MaxInt64,
	}
	for in, want := range cases {
		got, err := ParseBytes(in)
		if err != nil || got != want {
			t.Errorf("ParseBytes(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "MB", "12XB", "-5MB", "1.2.3MB", "1e20", "10000000t", "9223372036854775808000"} {
		if _, err := ParseBytes(bad); err == nil {
			t.Errorf("ParseBytes(%q) succeeded", bad)
		}
	}
}

func TestParseLimit(t *testing.T) {
	l, err := ParseLimit("10%")
	if err != nil || l.Percent != 10 || l.Resolve(500) != 50 || l.String() != "10%" || l.IsZero() {
		t.Fatalf("percent: %+v %v", l, err)
	}
	l, err = ParseLimit("50MB")
	if err != nil || l.Bytes != 50_000_000 || l.Resolve(1) != 50_000_000 || l.String() != "50 MB" {
		t.Fatalf("bytes: %+v %v", l, err)
	}
	// A percentage too large to resolve is no limit, not a wrapped one.
	if l, err := ParseLimit("1e300%"); err != nil || l.Resolve(1000) != math.MaxInt64 {
		t.Errorf("huge percent: %+v %v resolves to %d", l, err, l.Resolve(1000))
	}
	l, err = ParseLimit("  ")
	if err != nil || !l.IsZero() {
		t.Fatalf("empty: %+v %v", l, err)
	}
	for _, bad := range []string{"x%", "-1%", "fast", "NaN%", "Inf%", "-Inf%", "1e20"} {
		if _, err := ParseLimit(bad); err == nil {
			t.Errorf("ParseLimit(%q) succeeded", bad)
		}
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		n    int64
		iec  bool
		want string
	}{
		{0, false, "0 B"},
		{999, false, "999 B"},
		{1000, false, "1 KB"},
		{7_340_000, false, "7.3 MB"},
		{182_000_000, false, "182 MB"},
		{-4_000_000, false, "-4 MB"},
		{1 << 20, true, "1 MiB"},
		{5 << 40, false, "5.5 TB"},
		{9_999_000_000_000_000, false, "9999 TB"},
	}
	for _, c := range cases {
		if got := Format(c.n, c.iec); got != c.want {
			t.Errorf("Format(%d, %v) = %q, want %q", c.n, c.iec, got, c.want)
		}
	}
	if got := FormatDelta(38_000_000, false); got != "+38 MB" {
		t.Errorf("FormatDelta + = %q", got)
	}
	if got := FormatDelta(-38_000_000, false); got != "-38 MB" {
		t.Errorf("FormatDelta - = %q", got)
	}
	if got := FormatDelta(0, false); got != "0 B" {
		t.Errorf("FormatDelta 0 = %q", got)
	}
	if got := Percent(27, 100); got != "27%" {
		t.Errorf("Percent = %q", got)
	}
	if got := Percent(1, 0); got != "0%" {
		t.Errorf("Percent zero = %q", got)
	}
}
