package node

import (
	"context"
	"sync"
	"time"
)

// Paginated native threads keep their authoritative turns in the history database even while a rollout remains.
// A revert starts another rollout segment. Reading that segment alone loses the retained prefix.
var codexPagedDBs = struct {
	sync.Mutex
	byHome map[string]*codexAppDB
}{byHome: map[string]*codexAppDB{}}

func codexPaginatedThreads(home string) map[string]codexAppThread {
	if codexHistoryDBPath(home) == "" {
		return nil
	}
	db := codexAppDBPath(home)
	if db == "" {
		return nil
	}
	stamp := fileStamp(db) + fileStamp(db+"-wal")
	codexPagedDBs.Lock()
	c := codexPagedDBs.byHome[home]
	if c == nil {
		c = &codexAppDB{}
		codexPagedDBs.byHome[home] = c
	}
	if c.stamp == stamp || time.Since(c.at) < codexAppDBRefresh {
		defer codexPagedDBs.Unlock()
		return c.byID
	}
	c.at = time.Now()
	last := c.byID
	codexPagedDBs.Unlock()
	rows, err := readCodexThreads(context.Background(), db, `archived = 0 AND history_mode = 'paginated'`)
	if err != nil {
		return last // older schemas retain their existing rollout and App-history paths
	}
	for id, row := range rows {
		row.Paginated = true
		rows[id] = row
	}
	codexPagedDBs.Lock()
	c.byID, c.stamp = rows, stamp
	codexPagedDBs.Unlock()
	return rows
}

func codexPaginatedThreadOf(src codexSource) (codexAppThread, bool) {
	t, ok := codexPaginatedThreads(src.home)[src.thread]
	return t, ok
}

func invalidateCodexPaginated(home string) {
	codexPagedDBs.Lock()
	delete(codexPagedDBs.byHome, home)
	codexPagedDBs.Unlock()
	rolloutCaches.Lock()
	if c := rolloutCaches.byHome[home]; c != nil {
		c.at, c.olderAt = time.Time{}, time.Time{}
	}
	rolloutCaches.Unlock()
}
