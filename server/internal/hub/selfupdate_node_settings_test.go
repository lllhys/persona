package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/hubedge"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func nodePolicyRequest(t *testing.T, u *updater, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	u.routes(mux)
	path := "/api/v1/hub/update"
	if method == http.MethodPut {
		path += "/settings"
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

func TestNodePolicySettingsPersistenceAndFallback(t *testing.T) {
	u, st, original := updaterWithStoredSettings(t)
	u.h.cfg.AutoUpdateNodes = true
	if !u.nodesEnabled() {
		t.Fatal("unset persisted policy lost the startup opt-in")
	}
	for _, enabled := range []bool{false, true, false} {
		body, err := json.Marshal(map[string]bool{"auto_update_nodes": enabled})
		if err != nil {
			t.Fatal(err)
		}
		w := nodePolicyRequest(t, u, http.MethodPut, string(body))
		if w.Code != http.StatusOK {
			t.Fatalf("PUT: %d %s", w.Code, w.Body)
		}
		// A fresh updater loads the persisted choice, overriding the opposite
		// startup value and keeping unrelated update preferences and history.
		u.h.cfg.AutoUpdateNodes = !enabled
		u = newUpdater(u.h)
		u.h.upd = u
		if got := u.nodesEnabled(); got != enabled {
			t.Fatalf("after reload: policy=%v want %v", got, enabled)
		}
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			w = nodePolicyRequest(t, u, method, `{"auto_notice_seen":false,"auto_update_nodes":null}`)
			var view map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || view["auto_update_nodes"] != enabled {
				t.Fatalf("%s readback: %d %s", method, w.Code, w.Body)
			}
		}
		var saved updateSettings
		if _, err := st.Setting(context.Background(), updateSettingsKey, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.AutoUpdateNodes == nil || *saved.AutoUpdateNodes != enabled || saved.auto() != original.auto() ||
			!reflect.DeepEqual(saved.History, original.History) || !reflect.DeepEqual(saved.Bad, original.Bad) {
			t.Fatalf("persisted state changed unrelated settings: %+v", saved)
		}
		if got := u.h.devicesHello("m-one").AutoUpdateNodes; got != enabled {
			t.Fatalf("manager handshake policy=%v want %v", got, enabled)
		}
	}
}

func TestNodePolicySettingsFailuresStayDisabled(t *testing.T) {
	u, st, _ := updaterWithStoredSettings(t)
	st.readErr = errors.New("unavailable")
	u.h.cfg.AutoUpdateNodes = true
	if u.nodesEnabled() {
		t.Fatal("unread persisted policy enabled deployment")
	}
	if w := nodePolicyRequest(t, u, http.MethodPut, `{"auto_update_nodes":true}`); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("read failure: %d %s", w.Code, w.Body)
	}
	st.readErr = nil
	u.h.cfg.AutoUpdateNodes = false
	st.writeErr = errors.New("unavailable")
	if w := nodePolicyRequest(t, u, http.MethodPut, `{"auto_update_nodes":true}`); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("write failure: %d %s", w.Code, w.Body)
	}
	if u.nodesEnabled() || u.h.devicesConfig().AutoUpdateNodes {
		t.Fatal("failed save enabled deployment")
	}
	u.h.devices.mu.Lock()
	due := u.h.devices.due || u.h.devices.nodeUpdateDue
	u.h.devices.mu.Unlock()
	if due {
		t.Fatal("failed save scheduled a config broadcast or node update")
	}
	st.writeErr = nil
	for _, body := range []string{`{"auto_update_nodes":"true"}`, `{"auto_update_nodes":1}`} {
		if w := nodePolicyRequest(t, u, http.MethodPut, body); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid policy: %d %s", w.Code, w.Body)
		}
	}
}

// Real RPC pipes exercise event-before-request ordering without a database or
// any node deployment. Two managers retain their own machine lists.
func TestNodePolicyBroadcastAndExplicitPackageRecheck(t *testing.T) {
	h := newTestHub(t)
	h.devices.assignRead = true
	h.devices.assign = map[string]string{"two": "m-two"}
	h.cfg.Machines = []MachineConfig{{ID: "one"}, {ID: "two"}}
	var managers []nodePolicyManagerFixture
	for _, id := range []string{devicesOwner, "m-two"} {
		in, out := io.Pipe()
		remoteIn, remoteOut := io.Pipe()
		c := rosterConn(h, id, remoteOut)
		c.presence = busPresence{ID: id, Role: bus.RoleManager}
		remote := rpc.New("m", out)
		events := make(chan devicewire.Config, 8)
		checks := make(chan struct{}, 8)
		remote.OnEvent = func(event string, data json.RawMessage) {
			if event == devicewire.EventConfig {
				var cfg devicewire.Config
				if err := json.Unmarshal(data, &cfg); err != nil {
					t.Error(err)
					return
				}
				events <- cfg
			}
		}
		remote.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
			if method != devicewire.MethodUpdateNodes {
				return nil, errors.New("unexpected method " + method)
			}
			checks <- struct{}{}
			return map[string]any{}, nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(func() { cancel(); _ = out.Close(); _ = remoteOut.Close(); _ = in.Close(); _ = remoteIn.Close() })
		go func() { _ = c.peer.Serve(ctx, in) }()
		go func() { _ = remote.Serve(ctx, remoteIn) }()
		// Keep the channels on the connection's test record below.
		managers = append(managers, nodePolicyManagerFixture{id, events, checks})
	}
	for iteration, enabled := range []bool{true, true, false} {
		body, err := json.Marshal(map[string]bool{"auto_update_nodes": enabled})
		if err != nil {
			t.Fatal(err)
		}
		if w := nodePolicyRequest(t, h.upd, http.MethodPut, string(body)); w.Code != http.StatusOK {
			t.Fatalf("PUT: %d %s", w.Code, w.Body)
		}
		for _, m := range managers {
			if enabled {
				select {
				case <-m.checks:
				case <-time.After(5 * time.Second):
					t.Fatal("no package recheck", m.id)
				}
				if iteration == 0 {
					select {
					case cfg := <-m.events:
						if !cfg.AutoUpdateNodes || len(cfg.Machines) != 1 {
							t.Fatalf("missing policy or ownership: %+v", cfg)
						}
						var machine MachineConfig
						if err := json.Unmarshal(cfg.Machines[0], &machine); err != nil {
							t.Fatal(err)
						}
						want := "one"
						if m.id == "m-two" {
							want = "two"
						}
						if machine.ID != want {
							t.Fatalf("cross-manager ownership: got %s want %s", machine.ID, want)
						}
					default:
						t.Fatal("package request preceded policy config")
					}
				}
			} else {
				select {
				case cfg := <-m.events:
					if cfg.AutoUpdateNodes {
						t.Fatal("disable was not broadcast")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("no disable broadcast", m.id)
				}
				select {
				case <-m.checks:
					t.Fatal("disable requested a node update", m.id)
				default:
				}
			}
		}
	}
}

func TestNodePolicyPersistedEnableKeepsBusyProtection(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, m, _, _ := updatingHub(t, p, "release")
	h.slot = &hubedge.Slot{}
	setSessions(m, proto.Session{SessionID: "work", Status: proto.StatusWorking, Host: proto.EngineHerdr})
	if busy := h.updateBusy(); len(busy) != 0 {
		t.Fatalf("disabled policy held a Hub-only switch: %+v", busy)
	}
	if w := nodePolicyRequest(t, h.upd, http.MethodPut, `{"auto_update_nodes":true}`); w.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", w.Code, w.Body)
	}
	if h.cfg.AutoUpdateNodes || len(h.updateBusy()) != 1 {
		t.Fatal("persisted enable failed to protect the working remote agent")
	}
}

type nodePolicyManagerFixture struct {
	id     string
	events chan devicewire.Config
	checks chan struct{}
}

func TestNodePolicyPreservesManagedReleaseTrees(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			root, staged := t.TempDir(), t.TempDir()
			old := filepath.Join(root, "release-old")
			if err := os.Mkdir(old, 0o755); err != nil {
				t.Fatal(err)
			}
			name := "personad-linux-amd64"
			for _, suffix := range []string{"", ".version", ".prev", ".gz"} {
				if err := os.WriteFile(filepath.Join(old, name+suffix), []byte("immutable"+suffix), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			link := filepath.Join(root, "nodes")
			if err := os.Symlink(old, link); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PERSONA_NODE_BINARIES", link)
			if err := os.WriteFile(filepath.Join(staged, name), []byte("new release"), 0o755); err != nil {
				t.Fatal(err)
			}
			h := newTestHub(t)
			h.cfg.AutoUpdateNodes = enabled
			h.version = "new"
			h.upd.updateNodes(context.Background(), staged, "old")
			for _, suffix := range []string{"", ".version", ".prev", ".gz"} {
				got, err := os.ReadFile(filepath.Join(old, name+suffix))
				if err != nil || string(got) != "immutable"+suffix {
					t.Fatalf("release changed: %s %s %v", suffix, got, err)
				}
			}
			if got, err := os.Readlink(link); err != nil || got != old {
				t.Fatalf("pointer changed: %s %v", got, err)
			}
			if got, err := os.ReadFile(filepath.Join(staged, name)); err != nil || string(got) != "new release" {
				t.Fatal("candidate changed", err)
			}
		})
	}
}
