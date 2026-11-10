package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The Codex App keeps its threads in <CODEX_HOME>/state_<n>.sqlite (table threads). Since it moved their history into
// thread_history_1.sqlite (history_mode "paginated") it deletes their rollouts, so most chats in its sidebar have none and
// rollouts lists none of them (#3305: 12 of a sidebar's 21 chats were missing from Agents). codexAppThreads reads the rows of
// the chats the sidebar lists (codexAppState.sidebar): one the database has, unarchived, is listed as the sidebar lists it —
// through its rollout while the file is there, whatever its age, else from its row alone (title, directory, last update).
// An id in the sidebar's lists that the database lacks is a chat deleted in the App, which the sidebar does not show either.
//
// The database is read with the system sqlite3, read-only, as the Messages and Cindy readers are (cindybridge.go). Without
// sqlite3, without the database, or with a schema these columns are not in, nothing more is listed than before.

// codexAppDBRefresh: how often the database is looked at; it is read again only when it or the sidebar's lists changed.
const codexAppDBRefresh = 5 * time.Second

// codexAppSidebarMax bounds the ids put in one query; a sidebar lists far fewer.
const codexAppSidebarMax = 2000

type codexAppThread struct {
	ID          string `json:"id"`
	RolloutPath string `json:"rolloutPath"`
	Title       string `json:"title"`
	Cwd         string `json:"cwd"`
	UpdatedAtMs int64  `json:"updatedAtMs"`
}

func (t codexAppThread) updated() time.Time { return time.UnixMilli(t.UpdatedAtMs) }

type codexAppDB struct {
	at    time.Time // when the database was last looked at
	stamp string    // the database's files and the sidebar's ids when last read
	byID  map[string]codexAppThread
}

var codexAppDBs = struct {
	sync.Mutex
	byHome map[string]*codexAppDB
}{byHome: map[string]*codexAppDB{}}

// codexAppDBPath is the newest state database in home (state_5.sqlite in 2026-11): its number goes up with its schema.
func codexAppDBPath(home string) string {
	paths, _ := filepath.Glob(filepath.Join(home, "state_*.sqlite"))
	best, bestN := "", -1
	for _, p := range paths {
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "state_"), ".sqlite"))
		if err == nil && n > bestN {
			best, bestN = p, n
		}
	}
	return best
}

// codexAppThreads: the database's unarchived rows of the chats the Codex App's sidebar lists, by thread id; nil without
// the App's state or its database. The list is rebuilt every second and asks for each row: within codexAppDBRefresh the
// last answer is given without touching a file. A read that fails keeps the last rows read.
func codexAppThreads(home string) map[string]codexAppThread {
	codexAppDBs.Lock()
	c := codexAppDBs.byHome[home]
	if c == nil {
		c = &codexAppDB{}
		codexAppDBs.byHome[home] = c
	}
	now := time.Now()
	if now.Sub(c.at) < codexAppDBRefresh {
		defer codexAppDBs.Unlock()
		return c.byID
	}
	c.at = now // one caller looks; the others keep the last answer meanwhile
	last, lastStamp := c.byID, c.stamp
	codexAppDBs.Unlock()

	app := loadCodexAppState(home)
	db := ""
	if app != nil && len(app.sidebar) > 0 {
		db = codexAppDBPath(home)
	}
	stamp := ""
	if db != "" {
		stamp = fileStamp(db) + fileStamp(db+"-wal") + strings.Join(app.sidebar, ",")
	}
	if stamp == lastStamp {
		return last
	}
	var rows map[string]codexAppThread
	if db != "" {
		var err error
		if rows, err = readCodexAppThreads(context.Background(), db, app.sidebar); err != nil {
			return last
		}
	}
	codexAppDBs.Lock()
	defer codexAppDBs.Unlock()
	c.byID, c.stamp = rows, stamp
	return rows
}

func fileStamp(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "-;"
	}
	return fmt.Sprintf("%d.%d;", st.ModTime().UnixNano(), st.Size())
}

// readCodexAppThreads reads the unarchived rows of ids (validID only: they are put in the query as they are).
func readCodexAppThreads(ctx context.Context, db string, ids []string) (map[string]codexAppThread, error) {
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	q := `SELECT id, rollout_path AS rolloutPath, COALESCE(NULLIF(name, ''), title) AS title, cwd, ` +
		`COALESCE(updated_at_ms, updated_at * 1000) AS updatedAtMs FROM threads ` +
		`WHERE archived = 0 AND id IN ('` + strings.Join(ids, "', '") + `')`
	out, err := exec.CommandContext(cctx, sqlite, "-readonly", "-bail", "-json", "-cmd", ".timeout 2000", db, q).Output()
	if err != nil {
		return nil, err
	}
	byID := map[string]codexAppThread{}
	if len(bytes.TrimSpace(out)) == 0 { // no row: none of the sidebar's chats is in the database
		return byID, nil
	}
	var rows []codexAppThread
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		byID[r.ID] = r
	}
	return byID, nil
}

// codexAppThreadOf: src is a chat the Codex App's sidebar lists that has no rollout, and its row in the App's database.
func codexAppThreadOf(src codexSource) (codexAppThread, bool) {
	if src.path != "" {
		return codexAppThread{}, false
	}
	t, ok := codexAppThreads(src.home)[src.thread]
	return t, ok
}

// registerCodexAppThreads registers the chats the Codex App's sidebar lists, which rollouts misses when their rollout is
// older than its two weeks or gone: with the rollout the App's database names while it exists, else with none.
func (n *Node) registerCodexAppThreads(home string, pass *codexListPass) {
	threads := codexAppThreads(home)
	if len(threads) == 0 {
		return
	}
	canonical := canonicalCodexHome(home, n.home)
	for _, t := range threads {
		path := ""
		if t.RolloutPath != "" {
			if _, err := pass.stat(t.RolloutPath); err == nil {
				path = t.RolloutPath
			}
		}
		n.registerCanonicalCodex(canonical, t.ID, path)
	}
}
