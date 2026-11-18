package hub

import (
	"context"
	"encoding/json"
	"maps"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// The Persona apps' versions (docs/protocol/protocol.md "Client capabilities and heartbeat"): what each signed-in
// device last said about itself in client_hello, kept in the hub setting clientAppsSetting so the assistant can tell
// which build is installed while the app is in the background or offline. One entry per device credential: a
// simulator keeps its own and is marked once it registers as one, so it never stands in for the real phone.

const (
	clientAppsSetting = "hub.client_apps"
	clientAppsMax     = 32                   // the newest kept
	clientAppsMaxAge  = 180 * 24 * time.Hour // a device not seen for that long is dropped
	clientAppsResave  = time.Hour            // an unchanged report refreshes seen_at in the setting at most this often
)

// clientAppPlatforms are the apps whose version is their own; the web is left out (the hub serves it).
var clientAppPlatforms = map[string]bool{"ios": true, "macos": true, "watchos": true, "visionos": true, "android": true, "harmony": true}

type clientApps struct {
	mu        sync.Mutex
	loaded    bool
	byCred    map[string]assistantwire.ClientApp
	saved     map[string]time.Time // seen_at as last written
	pendingMu sync.Mutex
	pending   []clientAppUpdate
	flushing  bool
	changed   chan struct{}
}

// noteClientApp records what c said in client_hello (its meta is already set).
func (h *Hub) noteClientApp(c *client) {
	c.mu.Lock()
	meta := c.meta
	c.mu.Unlock()
	app, ok := clientAppOf(meta)
	if !ok {
		return
	}
	h.updateClientApp(c.device, func(prev assistantwire.ClientApp, had bool) assistantwire.ClientApp {
		app.Simulator = had && prev.Simulator
		return app
	})
}

// noteClientAppSimulator marks c's device as a simulator (register_phone says so after the hello).
func (h *Hub) noteClientAppSimulator(c *client, simulator bool) {
	if !simulator {
		return
	}
	h.updateClientApp(c.device, func(prev assistantwire.ClientApp, had bool) assistantwire.ClientApp {
		if !had {
			return prev
		}
		prev.Simulator = true
		return prev
	})
}

// updateClientApp records a change to the entry of credential cred. A persistent hub's consumer applies every queued
// change in message order and saves one snapshot for the whole batch; connection handlers never wait for the store.
func (h *Hub) updateClientApp(cred string, change func(assistantwire.ClientApp, bool) assistantwire.ClientApp) {
	// A pairing token is traded for a device credential within seconds: nothing is kept under it. Nor under the
	// hub's main token (no credential): a simulator on it is never marked, so it would pass for the phone.
	if cred == "" || strings.HasPrefix(cred, "pair_") {
		return
	}
	update := clientAppUpdate{cred: cred, change: change}
	if h.store == nil {
		h.writeClientApps([]clientAppUpdate{update})
		return
	}
	a := &h.apps
	h.startHelloWriters()
	if !h.admitHelloReport(h.helloWrites.appsSlot, helloStoreTimeout, &h.helloWrites.appsDrop, clientAppsSetting) {
		return
	}
	a.pendingMu.Lock()
	a.pending = append(a.pending, update)
	a.pendingMu.Unlock()
	wakeHelloWriter(h.helloWrites.appsWake)
}

// writeClientApps loads once, applies batch in order, then writes one snapshot. A failed load skips the whole batch:
// an unread setting is not an empty map, and saving it would drop every other device (#5827, #6889).
func (h *Hub) writeClientApps(batch []clientAppUpdate) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !h.prepareClientApps(ctx) {
		return
	}
	h.writeClientAppsLoaded(ctx, batch)
}

func (h *Hub) prepareClientApps(ctx context.Context) bool {
	a := &h.apps
	a.mu.Lock()
	defer a.mu.Unlock()
	return h.loadClientAppsLocked(ctx)
}

func (h *Hub) writeClientAppsLoaded(ctx context.Context, batch []clientAppUpdate) {
	a := &h.apps
	a.mu.Lock()
	save := false
	for _, update := range batch {
		if h.changeClientAppLocked(update) {
			save = true
		}
	}
	store := h.store
	if !save || store == nil {
		a.mu.Unlock()
		return
	}
	snapshot := maps.Clone(a.byCred)
	saved := make(map[string]time.Time, len(snapshot))
	for k, v := range snapshot {
		saved[k] = v.SeenAt
	}
	a.mu.Unlock()

	if err := store.PutSetting(ctx, clientAppsSetting, snapshot); err != nil {
		h.logf("save %s: %v", clientAppsSetting, err)
		return
	}
	a.mu.Lock()
	a.saved = saved
	a.mu.Unlock()
}

// changeClientAppLocked applies one report and reports whether the resulting snapshot needs a save.
func (h *Hub) changeClientAppLocked(update clientAppUpdate) bool {
	a := &h.apps
	prev, had := a.byCred[update.cred]
	next := update.change(prev, had)
	if next.Platform == "" {
		return false
	}
	if next.SeenAt.IsZero() {
		next.SeenAt = prev.SeenAt
	}
	a.byCred[update.cred] = next
	stale := next.SeenAt.Sub(a.saved[update.cred]) >= clientAppsResave
	same := had && prev.Platform == next.Platform && prev.Version == next.Version && prev.Build == next.Build &&
		prev.OS == next.OS && prev.Simulator == next.Simulator
	if same && !stale {
		return false
	}
	pruneClientApps(a.byCred, time.Now())
	return true
}

// loadClientAppsLocked publishes loaded state only after a complete successful read. The caller holds a.mu.
func (h *Hub) loadClientAppsLocked(ctx context.Context) bool {
	a := &h.apps
	if a.loaded {
		return true
	}
	if h.store == nil {
		if a.byCred == nil {
			a.byCred = map[string]assistantwire.ClientApp{}
		}
		if a.saved == nil {
			a.saved = map[string]time.Time{}
		}
		a.loaded = true
		return true
	}
	var stored map[string]assistantwire.ClientApp
	if _, err := h.store.Setting(ctx, clientAppsSetting, &stored); err != nil {
		h.logf("load %s: %v", clientAppsSetting, err)
		return false // tried again on the next report, without changing or saving partial state
	}
	byCred := make(map[string]assistantwire.ClientApp, len(stored))
	saved := make(map[string]time.Time, len(stored))
	for k, v := range stored {
		byCred[k], saved[k] = v, v.SeenAt
	}
	a.byCred, a.saved = byCred, saved
	a.loaded = true
	return true
}

// clientAppList is every kept app, newest first (devices.list "apps").
func (h *Hub) clientAppList(ctx context.Context) []assistantwire.ClientApp {
	a := &h.apps
	a.mu.Lock()
	defer a.mu.Unlock()
	h.loadClientAppsLocked(ctx)
	list := make([]assistantwire.ClientApp, 0, len(a.byCred))
	for _, app := range a.byCred {
		list = append(list, app)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].SeenAt.After(list[j].SeenAt) })
	return list
}

// pruneClientApps drops the devices not seen for clientAppsMaxAge and keeps the newest clientAppsMax.
func pruneClientApps(m map[string]assistantwire.ClientApp, now time.Time) {
	creds := make([]string, 0, len(m))
	for k, v := range m {
		if now.Sub(v.SeenAt) > clientAppsMaxAge {
			delete(m, k)
			continue
		}
		creds = append(creds, k)
	}
	if len(creds) <= clientAppsMax {
		return
	}
	sort.Slice(creds, func(i, j int) bool { return m[creds[i]].SeenAt.After(m[creds[j]].SeenAt) })
	for _, k := range creds[clientAppsMax:] {
		delete(m, k)
	}
}

// clientAppOf is the app a hello describes; false for the web, an extension, an unknown platform or no version.
// The Apple apps say what their requests' X-Persona-Client says ("ios-app", "1.4.0+52": the version, then the build);
// Android says "android", its versionName and its versionCode in build. The text goes into the assistant's prompt, so
// only short version-like tokens are kept.
func clientAppOf(meta clientMeta) (assistantwire.ClientApp, bool) {
	platform := strings.TrimSuffix(strings.ToLower(meta.Platform), "-app")
	version, build := meta.Version, buildText(meta.Build)
	if v, b, ok := strings.Cut(version, "+"); ok && build == "" {
		version, build = v, b
	}
	version = versionToken(version)
	if !clientAppPlatforms[platform] || version == "" {
		return assistantwire.ClientApp{}, false
	}
	app := assistantwire.ClientApp{Platform: platform, Version: version, Build: versionToken(build),
		SeenAt: time.Now().UTC().Truncate(time.Second)}
	if name := versionToken(meta.OS.Name); name != "" {
		app.OS = strings.TrimSpace(name + " " + versionToken(meta.OS.Version))
	}
	return app, true
}

// buildText is build as text: a JSON string or number (Android sends its versionCode, the Apple apps CFBundleVersion).
func buildText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		if _, err := strconv.ParseFloat(n.String(), 64); err == nil {
			return n.String()
		}
	}
	return ""
}

// versionToken is s if it is a short run of letters, digits and . _ + - (at most 32), else "".
func versionToken(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 32 || strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._+-") != "" {
		return ""
	}
	return s
}
