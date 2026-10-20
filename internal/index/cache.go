package index

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// cacheFormat versions the on-disk encoding; bump it when Layer changes.
const cacheFormat = "v1"

// Cache stores layer indexes by diffID under Dir, zstd-compressed gob,
// capped at MaxBytes (oldest used first out). Writes are atomic.
type Cache struct {
	Dir      string
	MaxBytes int64
}

// CacheEntry describes one cached layer index.
type CacheEntry struct {
	DiffID string    `json:"diff_id"`
	Bytes  int64     `json:"bytes"`
	Used   time.Time `json:"used"`
}

func (c *Cache) dir() string { return filepath.Join(c.Dir, "layers", cacheFormat) }

func (c *Cache) file(diffID string) string {
	return filepath.Join(c.dir(), strings.ReplaceAll(diffID, ":", "-")+".idx")
}

// Get returns the cached index for diffID, if present and readable.
func (c *Cache) Get(diffID string) (*Layer, bool) {
	f := c.file(diffID)
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, false
	}
	dec, err := zstd.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, false
	}
	defer dec.Close()
	var l Layer
	if err := gob.NewDecoder(dec).Decode(&l); err != nil || l.DiffID != diffID {
		return nil, false
	}
	now := time.Now()
	_ = os.Chtimes(f, now, now) // mark as recently used
	return &l, true
}

// Put stores l and then trims the cache to MaxBytes.
func (c *Cache) Put(l *Layer) error {
	if err := os.MkdirAll(c.dir(), 0o755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	var buf bytes.Buffer
	enc, _ := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err := gob.NewEncoder(enc).Encode(l); err != nil {
		return fmt.Errorf("encode layer index: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode layer index: %w", err)
	}
	tmp, err := os.CreateTemp(c.dir(), ".tmp-*")
	if err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	_, err = tmp.Write(buf.Bytes())
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("write cache: %w", err)
	}
	if err := os.Rename(tmp.Name(), c.file(l.DiffID)); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("write cache: %w", err)
	}
	if c.MaxBytes > 0 {
		_, err = c.Prune(c.MaxBytes)
	}
	return err
}

// List returns the cached indexes, most recently used first.
func (c *Cache) List() ([]CacheEntry, error) {
	des, err := os.ReadDir(c.dir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cache: %w", err)
	}
	var out []CacheEntry
	for _, de := range des {
		name, ok := strings.CutSuffix(de.Name(), ".idx")
		if !ok || de.IsDir() {
			continue
		}
		fi, err := de.Info()
		if err != nil {
			continue
		}
		out = append(out, CacheEntry{DiffID: strings.Replace(name, "-", ":", 1), Bytes: fi.Size(), Used: fi.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Used.Equal(out[j].Used) {
			return out[i].Used.After(out[j].Used)
		}
		return out[i].DiffID < out[j].DiffID
	})
	return out, nil
}

// Prune deletes the least recently used indexes until the cache holds at
// most max bytes, and returns what it deleted. max 0 deletes everything.
func (c *Cache) Prune(max int64) ([]CacheEntry, error) {
	entries, err := c.List()
	if err != nil {
		return nil, err
	}
	var total int64
	for _, e := range entries {
		total += e.Bytes
	}
	var removed []CacheEntry
	for i := len(entries) - 1; i >= 0 && total > max; i-- {
		if err := os.Remove(c.file(entries[i].DiffID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("prune cache: %w", err)
		}
		total -= entries[i].Bytes
		removed = append(removed, entries[i])
	}
	return removed, nil
}
