package node

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A Unix control endpoint whose app-server never came up is not dialled every 5 s for good (#2439): discovered
// endpoints are never forgotten (the manager keeps them in the inventory it gives back), and each failed attempt
// ended in a whole session list for the manager. While its socket looks the same it waits 5, 10, 20, 40 s, then
// 60 s between attempts; any change to the socket (it appears, is replaced, its directory or alias target changes)
// is tried at once, so a restarted app-server is joined as fast as before. WebSocket endpoints keep the 5 s retry.
const (
	codexRetryFirst = 5 * time.Second
	codexRetryMost  = time.Minute
	// codexRetrySlack: runCodex's calls come every 5 s, later by its own work in between; an attempt due within half
	// a tick goes with this call rather than the next.
	codexRetrySlack = codexRetryFirst / 2
)

// codexRetry is an endpoint's attempts that did not reach the handshake, since the last that did.
type codexRetry struct {
	spec  proto.CodexServer // what was dialled
	at    time.Time         // when the last attempt started
	print string            // the socket as it looked then (codexEndpointPrint)
	fails int
}

// codexRetryWait is how long after an attempt the next goes, fails attempts in a row having failed.
func codexRetryWait(fails int) time.Duration {
	if fails > 5 { // 5 s << 4 is past the most already
		return codexRetryMost
	}
	return min(codexRetryFirst<<max(fails-1, 0), codexRetryMost)
}

// retryDue: spec may be dialled at now, its socket looking like print. Callers hold r.mu.
func (r *codexRegistry) retryDue(spec proto.CodexServer, now time.Time, print string) bool {
	b, ok := r.retries[spec.ID]
	if !ok || b.fails == 0 || b.spec != spec || print == "" || print != b.print {
		return true
	}
	return now.Sub(b.at) >= codexRetryWait(b.fails)-codexRetrySlack
}

// attempted: spec is being dialled from now, its socket looking like print. Callers hold r.mu.
func (r *codexRegistry) attempted(spec proto.CodexServer, now time.Time, print string) {
	b := r.retries[spec.ID]
	if b.spec != spec {
		b = codexRetry{spec: spec}
	}
	b.at, b.print = now, print
	if r.retries == nil {
		r.retries = map[string]codexRetry{}
	}
	r.retries[spec.ID] = b
}

// attemptEnded: the connection to spec ended; reached: its handshake had gone through. Callers hold r.mu.
func (r *codexRegistry) attemptEnded(spec proto.CodexServer, reached bool) {
	b, ok := r.retries[spec.ID]
	if !ok || b.spec != spec {
		return
	}
	if reached {
		delete(r.retries, spec.ID)
		return
	}
	b.fails++
	r.retries[spec.ID] = b
}

// codexEndpointPrint is how a Unix control endpoint's socket looks now: its directory and the path, and for an alias
// (codexSocketPath) the link's target and the target's directory, each as missing or its inode, mode and mtime. ""
// for any other endpoint.
func codexEndpointPrint(endpoint string) string {
	path, ok := strings.CutPrefix(endpoint, "unix://")
	if !ok || path == "" {
		return ""
	}
	var b strings.Builder
	look := func(p string) {
		st, err := os.Lstat(p)
		if err != nil {
			b.WriteString("-|")
			return
		}
		b.WriteString(strconv.FormatUint(fileInode(st), 10))
		b.WriteByte('/')
		b.WriteString(strconv.FormatUint(uint64(st.Mode()), 8))
		b.WriteByte('/')
		b.WriteString(strconv.FormatInt(st.ModTime().UnixNano(), 10))
		b.WriteByte('|')
	}
	look(filepath.Dir(path))
	look(path)
	if target, err := os.Readlink(path); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		b.WriteString(target)
		b.WriteByte('|')
		look(filepath.Dir(target))
		look(target)
	}
	return b.String()
}
