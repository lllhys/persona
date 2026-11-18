package remoteimage

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// diskCache keeps sources and the sizes drawn from them, one file each named by its key: a first line "<type> <width>
// <height>", then the bytes. It is a cache, not state: the directory may be deleted at any time. A read marks the file
// used (its modification time); past max bytes the least recently used go until it holds 80% of max. A nil
// *diskCache keeps nothing.
type diskCache struct {
	dir  string
	max  int64
	logf func(string, ...any)

	mu      sync.Mutex
	counted bool  // total was read from the directory
	total   int64 // bytes in the directory, as far as this process knows
}

func (c *diskCache) path(k string) string { return filepath.Join(c.dir, k) }

// image is the file under k, or nil.
func (c *diskCache) image(k string) *Image {
	if c == nil {
		return nil
	}
	data, err := os.ReadFile(c.path(k))
	if err != nil {
		return nil
	}
	nl := bytes.IndexByte(data, '\n')
	if nl < 0 {
		return nil
	}
	img := &Image{}
	if _, err := fmt.Sscanf(string(data[:nl]), "%s %d %d", &img.Type, &img.Width, &img.Height); err != nil {
		return nil
	}
	img.Data = data[nl+1:]
	now := time.Now()
	_ = os.Chtimes(c.path(k), now, now)
	return img
}

// put keeps img under k, then trims the directory if it went over max.
func (c *diskCache) put(k string, img *Image) {
	if c == nil {
		return
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		c.logf("remote images: cache: %v", err)
		return
	}
	head := fmt.Sprintf("%s %d %d\n", img.Type, img.Width, img.Height)
	tmp, err := os.CreateTemp(c.dir, ".put-*")
	if err != nil {
		c.logf("remote images: cache: %v", err)
		return
	}
	_, err = tmp.WriteString(head)
	if err == nil {
		_, err = tmp.Write(img.Data)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), c.path(k))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		c.logf("remote images: cache: %v", err)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.counted {
		c.total, c.counted = c.size(), true
	} else {
		c.total += int64(len(head) + len(img.Data))
	}
	if c.total > c.max {
		c.trim()
	}
}

type cached struct {
	name string
	size int64
	used time.Time
}

func (c *diskCache) list() []cached {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return nil
	}
	out := make([]cached, 0, len(entries))
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			out = append(out, cached{e.Name(), info.Size(), info.ModTime()})
		}
	}
	return out
}

func (c *diskCache) size() int64 {
	var n int64
	for _, f := range c.list() {
		n += f.size
	}
	return n
}

// trim removes the least recently used files until the directory holds 80% of max (counted again from the
// directory, so files removed by hand or another process are not counted twice).
func (c *diskCache) trim() {
	files := c.list()
	slices.SortFunc(files, func(a, b cached) int { return a.used.Compare(b.used) })
	var total int64
	for _, f := range files {
		total += f.size
	}
	goal := c.max * 8 / 10
	for _, f := range files {
		if total <= goal {
			break
		}
		if err := os.Remove(c.path(f.name)); err == nil || os.IsNotExist(err) {
			total -= f.size
		}
	}
	c.total = total
}

// limiter allows each caller n events per window, as a bucket that refills evenly; callers not seen for a window are
// forgotten.
type limiter struct {
	n      float64
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newLimiter(n int, window time.Duration, now func() time.Time) *limiter {
	return &limiter{n: float64(n), window: window, now: now, buckets: map[string]*bucket{}}
}

// take spends one of caller's events: false, with how long until the next, when none is left.
func (l *limiter) take(caller string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	rate := l.n / l.window.Seconds() // per second
	if len(l.buckets) > 256 {
		for k, b := range l.buckets {
			if now.Sub(b.at) > l.window {
				delete(l.buckets, k)
			}
		}
	}
	b := l.buckets[caller]
	if b == nil {
		b = &bucket{tokens: l.n, at: now}
		l.buckets[caller] = b
	}
	b.tokens = min(l.n, b.tokens+now.Sub(b.at).Seconds()*rate)
	b.at = now
	if b.tokens < 1 {
		return time.Duration((1 - b.tokens) / rate * float64(time.Second)), false
	}
	b.tokens--
	return 0, true
}
