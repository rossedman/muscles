// Package size parses and formats byte counts and size limits such as
// "800MB", "1.5GiB" and "10%".
package size

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

var units = map[string]float64{
	"":    1,
	"b":   1,
	"k":   1e3,
	"kb":  1e3,
	"m":   1e6,
	"mb":  1e6,
	"g":   1e9,
	"gb":  1e9,
	"t":   1e12,
	"tb":  1e12,
	"kib": 1 << 10,
	"mib": 1 << 20,
	"gib": 1 << 30,
	"tib": 1 << 40,
}

// maxInt64 is 2^63, the first float64 too large to convert to an int64.
const maxInt64 = float64(1 << 63)

// ParseBytes parses a byte count: a number with an optional SI (KB, MB, GB,
// TB) or IEC (KiB, MiB, GiB, TiB) suffix. "1.5GB" is 1,500,000,000 bytes.
func ParseBytes(s string) (int64, error) {
	t := strings.TrimSpace(s)
	i := len(t)
	for i > 0 && (t[i-1] < '0' || t[i-1] > '9') {
		i--
	}
	num, unit := strings.TrimSpace(t[:i]), strings.ToLower(strings.TrimSpace(t[i:]))
	mult, ok := units[unit]
	if !ok || num == "" {
		return 0, fmt.Errorf("invalid size %q (use a number with an optional unit, like 800MB or 1.5GiB)", s)
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("invalid size %q (use a number with an optional unit, like 800MB or 1.5GiB)", s)
	}
	switch n := math.Round(f * mult); {
	case n > maxInt64:
		// Converting would wrap (to a negative budget on amd64).
		return 0, fmt.Errorf("size %q is too large (the most is about 9.2 EB)", s)
	case n == maxInt64:
		// The nearest float64 to MaxInt64, as Format(math.MaxInt64) prints it.
		return math.MaxInt64, nil
	default:
		return int64(n), nil
	}
}

// Limit is a size budget: an absolute byte count or a percentage of a
// reference size. The zero Limit means "not set".
type Limit struct {
	Bytes   int64
	Percent float64
}

// ParseLimit parses "50MB" or "10%". An empty string is the zero Limit.
func ParseLimit(s string) (Limit, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return Limit{}, nil
	}
	if p, ok := strings.CutSuffix(t, "%"); ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
			return Limit{}, fmt.Errorf("invalid percentage %q (use a number like 10%%)", s)
		}
		return Limit{Percent: f}, nil
	}
	b, err := ParseBytes(t)
	if err != nil {
		return Limit{}, err
	}
	return Limit{Bytes: b}, nil
}

// IsZero reports whether the limit is unset.
func (l Limit) IsZero() bool { return l.Bytes == 0 && l.Percent == 0 }

// Resolve returns the limit in bytes, taking a percentage of of.
func (l Limit) Resolve(of int64) int64 {
	if l.Percent != 0 {
		if n := math.Round(float64(of) * l.Percent / 100); n < maxInt64 {
			return int64(n)
		}
		return math.MaxInt64 // a percentage that large is no limit at all
	}
	return l.Bytes
}

// String formats the limit the way a user would write it.
func (l Limit) String() string {
	if l.Percent != 0 {
		return strconv.FormatFloat(l.Percent, 'f', -1, 64) + "%"
	}
	return Format(l.Bytes, false)
}

// Format renders n bytes for people: "512 B", "7.3 MB", "182 MB". iec uses
// powers of 1024 (KiB, MiB, ...) instead of 1000.
func Format(n int64, iec bool) string {
	base, suffixes := 1000.0, []string{"B", "KB", "MB", "GB", "TB"}
	if iec {
		base, suffixes = 1024.0, []string{"B", "KiB", "MiB", "GiB", "TiB"}
	}
	neg := n < 0
	f := math.Abs(float64(n))
	i := 0
	for f >= base && i < len(suffixes)-1 {
		f /= base
		i++
	}
	var s string
	switch {
	case i == 0:
		s = strconv.FormatFloat(f, 'f', 0, 64)
	case f < 10:
		s = strconv.FormatFloat(math.Round(f*10)/10, 'f', -1, 64)
	default:
		s = strconv.FormatFloat(math.Round(f), 'f', 0, 64)
	}
	if neg {
		s = "-" + s
	}
	return s + " " + suffixes[i]
}

// FormatDelta renders a signed change: "+38 MB", "-4 MB", "0 B".
func FormatDelta(n int64, iec bool) string {
	if n > 0 {
		return "+" + Format(n, iec)
	}
	return Format(n, iec)
}

// Percent renders part/whole as "27%", or "0%" when whole is zero.
func Percent(part, whole int64) string {
	if whole == 0 {
		return "0%"
	}
	return strconv.FormatFloat(math.Round(float64(part)*100/float64(whole)), 'f', 0, 64) + "%"
}
