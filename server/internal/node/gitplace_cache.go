package node

import (
	"sync"
	"time"
)

const (
	gitPlaceTTL   = 10 * time.Second
	gitPlaceLimit = 1024
	// A filesystem open can wait indefinitely for a mount or a privacy prompt.
	// Optional labels must neither hold up inventory nor accumulate readers.
	gitPlaceReaders = 4
)

type gitPlaceEntry struct {
	place   gitPlace
	at      time.Time
	pending chan struct{}
}

type gitPlaceCache struct {
	mu      sync.Mutex
	entries map[string]*gitPlaceEntry
	active  int
	read    func(string) gitPlace
}

var gitPlaces = newGitPlaceCache(readGitPlace)

func newGitPlaceCache(read func(string) gitPlace) *gitPlaceCache {
	return &gitPlaceCache{entries: make(map[string]*gitPlaceEntry), read: read}
}

// lookup returns the last completed labels immediately. A cold or stale lookup
// refreshes in the background; session discovery and busy state never depend on
// a successful metadata read. Pending entries are never evicted or duplicated.
func (c *gitPlaceCache) lookup(cwd string) gitPlace {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[cwd]
	if e != nil && (e.pending != nil || time.Since(e.at) < gitPlaceTTL) {
		return e.place
	}
	if c.active >= gitPlaceReaders {
		if e != nil {
			return e.place
		}
		return gitPlace{}
	}
	if e == nil {
		if len(c.entries) >= gitPlaceLimit {
			var oldest string
			var at time.Time
			for key, entry := range c.entries {
				if entry.pending == nil && (at.IsZero() || entry.at.Before(at)) {
					oldest, at = key, entry.at
				}
			}
			delete(c.entries, oldest)
		}
		e = &gitPlaceEntry{}
		c.entries[cwd] = e
	}
	e.pending = make(chan struct{})
	c.active++
	go func() {
		p := c.read(cwd)
		c.mu.Lock()
		e.place, e.at = p, time.Now()
		close(e.pending)
		e.pending = nil
		c.active--
		c.mu.Unlock()
	}()
	return e.place
}
