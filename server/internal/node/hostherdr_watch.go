package node

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/host"
)

// Watch is one events.subscribe per server: the lifecycle events plus agent status for each of its panes (herdr only
// sends those per pane). A status change into or out of blocked is urgent. It ends when the own server's stream does.
// The named sessions watched are the running ones and those the panes are in, looked at again every sessionsEvery (a
// session started since gets its stream, a stopped one's ends); a named session's stream that ends is opened again
// (the events lost in between are covered by the snapshot its first event brings).
func (h *herdrHost) Watch(ctx context.Context, panes []host.PaneRef, changed func(urgent bool)) error {
	byServer := map[string][]string{}
	for _, p := range panes {
		session, id := herdr.SplitID(p.ID)
		byServer[session] = append(byServer[session], id)
	}
	if h.named == nil {
		return watchServer(ctx, h.c, byServer[""], changed)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex // changed is called by one stream at a time, as with one server
	one := func(urgent bool) {
		mu.Lock()
		defer mu.Unlock()
		changed(urgent)
	}
	own := make(chan error, 1)
	go func() { own <- watchServer(ctx, h.c, byServer[""], one) }()

	type watched struct {
		ns   *herdrSession
		stop context.CancelFunc
	}
	streams := map[string]watched{}
	var wg sync.WaitGroup
	reconcile := func() {
		want := map[string]*herdrSession{}
		for _, ns := range h.named.running(ctx, time.Now().Add(namedWait)) {
			want[ns.name] = ns
		}
		for name := range byServer {
			if name != "" && want[name] == nil {
				if ns := h.named.lookup(ctx, name); ns != nil {
					want[name] = ns
				}
			}
		}
		for name, w := range streams {
			if want[name] != w.ns { // stopped, or a new server under the same name
				w.stop()
				delete(streams, name)
			}
		}
		for name, ns := range want {
			if _, ok := streams[name]; ok {
				continue
			}
			sctx, stop := context.WithCancel(ctx)
			streams[name] = watched{ns: ns, stop: stop}
			wg.Add(1)
			go func() {
				defer wg.Done()
				watchNamed(sctx, ns.c, byServer[name], one)
			}()
		}
	}
	// reconcile may wait for herdr's list of sessions: it runs beside the loop (one at a time), so the own stream
	// ending is seen at once.
	var reconciling sync.Mutex
	again := func() {
		if !reconciling.TryLock() {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer reconciling.Unlock()
			reconcile()
		}()
	}
	defer wg.Wait()
	defer cancel()
	again()
	tick := time.NewTicker(sessionsEvery)
	defer tick.Stop()
	for {
		select {
		case err := <-own:
			return err
		case <-tick.C:
			again()
		}
	}
}

// watchNamed keeps a named session's stream open until ctx ends, opening it again with a growing pause when it ends.
func watchNamed(ctx context.Context, c *herdr.Client, panes []string, changed func(urgent bool)) {
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		_ = watchServer(ctx, c, panes, changed)
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// watchServer is one events.subscribe on c for panes (herdr's ids there).
func watchServer(ctx context.Context, c *herdr.Client, panes []string, changed func(urgent bool)) error {
	var w waits
	return c.Subscribe(ctx, subscriptions(panes), func(e herdr.Event) {
		var d struct {
			PaneID      string `json:"pane_id"`
			AgentStatus string `json:"agent_status"`
		}
		status := e.Event == "pane.agent_status_changed" || e.Event == "pane_agent_status_changed"
		changed(status && json.Unmarshal(e.Data, &d) == nil && w.changed(d.PaneID, d.AgentStatus == "blocked"))
	})
}

// herdrLifecycle are the events that change which sessions there are or how they are named.
var herdrLifecycle = []string{
	"workspace.created", "workspace.renamed", "workspace.closed", "workspace.moved",
	"tab.created", "tab.renamed", "tab.closed", "tab.moved",
	"pane.created", "pane.closed", "pane.exited", "pane.updated", "pane.moved", "pane.agent_detected",
}

// subscriptions: the lifecycle events plus agent status for each pane (herdr only sends those per pane).
func subscriptions(panes []string) []herdr.Subscription {
	subs := make([]herdr.Subscription, 0, len(herdrLifecycle)+len(panes))
	for _, t := range herdrLifecycle {
		subs = append(subs, herdr.Subscription{"type": t})
	}
	for _, p := range panes {
		subs = append(subs, herdr.Subscription{"type": "pane.agent_status_changed", "pane_id": p})
	}
	return subs
}
