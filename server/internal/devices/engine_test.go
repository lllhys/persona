package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// settingsStore keeps settings in memory like the settings table.
type settingsStore struct {
	Store
	mu   sync.Mutex
	vals map[string][]byte
	puts int
}

func (s *settingsStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.vals[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, out)
}

func (s *settingsStore) PutSetting(_ context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vals[key] = b
	s.puts++
	return nil
}

func (s *settingsStore) TakeSetting(ctx context.Context, key string, out any) (bool, error) {
	return s.Setting(ctx, key, out)
}

// engineNode is a node answering the hub over pipes, recording the methods it was called with.
type engineNode struct {
	mu    sync.Mutex
	calls []string
}

func (f *engineNode) handle(_ context.Context, method string, params json.RawMessage) (any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, method+" "+string(params))
	f.mu.Unlock()
	if method == "sessions.list" {
		return map[string]any{"sessions": []proto.Session{}}, nil
	}
	return map[string]any{}, nil
}

func (f *engineNode) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func engineHub(t *testing.T) (*Devices, *settingsStore, *Machine, http.Handler) {
	t.Helper()
	h := newTestDevices(t)
	st := &settingsStore{vals: map[string][]byte{}}
	h.opts.Store = st
	m := newMachine(h, MachineConfig{ID: "m1", Name: "box"})
	h.addEntry(m)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/machines/{mid}/engine", h.withMachine(h.putEngine))
	mux.HandleFunc("GET /api/v1/machines", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"machines": h.machineList()})
	})
	return h, st, m, mux
}

// connectEngineNode gives m a node connection.
func connectEngineNode(t *testing.T, m *Machine) (*engineNode, *rpc.Peer) {
	t.Helper()
	f := &engineNode{}
	toNode, nodeIn := io.Pipe()
	toHub, hubIn := io.Pipe()
	hubPeer := rpc.New("h", nodeIn)
	nodePeer := rpc.New("n", hubIn)
	nodePeer.OnRequest = f.handle
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); toNode.Close(); toHub.Close() })
	go hubPeer.Serve(ctx, toHub)
	go nodePeer.Serve(ctx, toNode)
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.info.Engines = []proto.Engine{{ID: "herdr", Name: "herdr", Available: true, Version: "0.9.3"},
		{ID: "herdr-core", Name: "Herdr App", Available: true, Version: "0.1.16"}}
	m.mu.Unlock()
	return f, hubPeer
}

func put(t *testing.T, mux http.Handler, path, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("PUT", path, strings.NewReader(body)))
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// Offline: herdr by default, engines empty; a PUT is stored (settings, one key) and answered with the machine; bad
// engines and unknown machines are refused.
func TestEnginePutOffline(t *testing.T) {
	h, st, _, mux := engineHub(t)
	ms := h.machineList()
	b, _ := json.Marshal(ms[0])
	if ms[0].Engine != "herdr" || !strings.Contains(string(b), `"engine":"herdr","engines":[]`) {
		t.Errorf("default machine: %s", b)
	}
	code, out := put(t, mux, "/api/v1/machines/m1/engine", `{"engine":"herdr-core"}`)
	if code != 200 || out["engine"] != "herdr-core" || out["id"] != "m1" {
		t.Fatalf("PUT = %d %v", code, out)
	}
	var stored map[string]string
	if ok, _ := st.Setting(context.Background(), enginesKey, &stored); !ok || stored["m1"] != "herdr-core" {
		t.Errorf("stored %v", stored)
	}
	if code, out := put(t, mux, "/api/v1/machines/m1/engine", `{"engine":"tmux"}`); code != 400 || out["error"] == nil {
		t.Errorf("unknown engine: %d %v", code, out)
	}
	if code, _ := put(t, mux, "/api/v1/machines/m1/engine", `nope`); code != 400 {
		t.Errorf("bad body: %d", code)
	}
	if code, _ := put(t, mux, "/api/v1/machines/zz/engine", `{"engine":"herdr"}`); code != 404 {
		t.Errorf("unknown machine: %d", code)
	}
	if st.puts != 2 { // one choice: the set and, beside it, the single engine (engine_hosts_test.go)
		t.Errorf("%d writes", st.puts)
	}

	// A new hub reads it back from settings.
	h2 := newTestDevices(t)
	h2.opts.Store = st
	if got := h2.engineOf("m1"); got != "herdr-core" {
		t.Errorf("after a restart: %s", got)
	}
	if got := h2.engineOf("other"); got != "herdr" {
		t.Errorf("never set: %s", got)
	}
}

// Online: a PUT is sent to the node at once, the machine lists what the node found.
func TestEngineOnline(t *testing.T) {
	_, _, m, mux := engineHub(t)
	f, peer := connectEngineNode(t, m)
	code, out := put(t, mux, "/api/v1/machines/m1/engine", `{"engine":"herdr-core"}`)
	if code != 200 || out["engine"] != "herdr-core" {
		t.Fatalf("PUT = %d %v", code, out)
	}
	if calls := f.seen(); len(calls) != 1 || calls[0] != `engine.use {"engine":"herdr-core","hosts":["herdr-core"],"default":"herdr-core"}` {
		t.Errorf("node got %v", calls)
	}
	engines, _ := out["engines"].([]any)
	if len(engines) != 2 || engines[1].(map[string]any)["name"] != "Herdr App" {
		t.Errorf("engines %v", out["engines"])
	}
	// configureNode no longer sends it: connect does, first (TestEngineConnectOrder).
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
	m.configureNode(context.Background(), peer, nil, true)
	for _, c := range f.seen() {
		if strings.HasPrefix(c, "engine.use") {
			t.Errorf("configureNode sent %s", c)
		}
	}
}

// The machine a PUT answers, which the app takes as it is, has the notes on its engines in the reader's language, as
// the hub's machine list does (#8134); the node's report keeps the canonical text.
func TestEnginePutAnswersNotesInTheReadersLanguage(t *testing.T) {
	_, _, m, mux := engineHub(t)
	connectEngineNode(t, m)
	args := i18n.Args{"version": "0.14.0", "minimum": "0.15.0"}
	note := i18n.T(i18n.Default, "node-engine.paseo_outdated", args)
	m.mu.Lock()
	m.info.Engines = append(m.info.Engines, proto.Engine{ID: proto.EnginePaseo, Name: "Paseo", Available: true, Version: "0.14.0", Note: note})
	m.mu.Unlock()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/v1/machines/m1/engine", strings.NewReader(`{"hosts":["paseo"]}`))
	r.Header.Set("Accept-Language", "en")
	mux.ServeHTTP(rec, r)
	var out proto.Machine
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	i := slices.IndexFunc(out.Engines, func(e proto.Engine) bool { return e.ID == proto.EnginePaseo })
	if i < 0 || out.Engines[i].Note != i18n.T(i18n.En, "node-engine.paseo_outdated", args) {
		t.Fatalf("engines %+v", out.Engines)
	}
	m.mu.Lock()
	kept := m.info.Engines[len(m.info.Engines)-1].Note
	m.mu.Unlock()
	if kept != note {
		t.Fatalf("the node's report changed: %q", kept)
	}
}

// A machine never set gets no engine.use (its node starts on herdr; old nodes never see the method).
func TestEngineNotSentWhenNeverChosen(t *testing.T) {
	_, _, m, _ := engineHub(t)
	f, peer := connectEngineNode(t, m)
	m.useEngine(context.Background(), peer)
	if calls := f.seen(); len(calls) != 0 {
		t.Errorf("node got %v", calls)
	}
}

// Settings that cannot be read are not read again on every machine list: herdr meanwhile, retried later.
func TestEngineLoadBacksOff(t *testing.T) {
	h := newTestDevices(t)
	st := &failingSettings{}
	h.opts.Store = st
	for range 5 {
		if got := h.engineOf("m1"); got != "herdr" {
			t.Fatalf("engineOf = %s", got)
		}
	}
	if st.reads != 1 {
		t.Errorf("%d reads", st.reads)
	}
}

type failingSettings struct {
	Store
	reads int
}

func (s *failingSettings) Setting(context.Context, string, any) (bool, error) {
	s.reads++
	return false, errors.New("db down")
}

// TestEngineNodeHelper is a node for TestEngineConnectOrder: it writes every method it is called with to a file.
func TestEngineNodeHelper(t *testing.T) {
	path := os.Getenv("PERSONA_TEST_ENGINE_NODE")
	if path == "" {
		return
	}
	peer := rpc.New("n", os.Stdout)
	peer.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		fmt.Fprintln(f, method)
		f.Close()
		switch method {
		case "node.info":
			return proto.NodeInfo{Hostname: "test-node"}, nil
		case "sessions.list":
			return map[string]any{"sessions": []proto.Session{}}, nil
		}
		return map[string]any{}, nil
	}
	_ = peer.Serve(context.Background(), os.Stdin)
	os.Exit(0)
}

// On connect a stored engine goes to the node before anything else, node.info included.
func TestEngineConnectOrder(t *testing.T) {
	for _, stored := range []bool{true, false} {
		h, _, m, _ := engineHub(t)
		if stored {
			if err := h.setEngine(context.Background(), "m1", "herdr-core"); err != nil {
				t.Fatal(err)
			}
		}
		bin, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		log := filepath.Join(t.TempDir(), "calls")
		m.cfg.Kind = "local"
		m.nodeCmd = func(ctx context.Context) *exec.Cmd {
			cmd := exec.CommandContext(ctx, bin, "-test.run", "^TestEngineNodeHelper$")
			cmd.Env = append(os.Environ(), "PERSONA_TEST_ENGINE_NODE="+log)
			return cmd
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); m.run(ctx) }()
		var calls []string
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			b, _ := os.ReadFile(log)
			calls = strings.Fields(string(b))
			if slices.Contains(calls, "sessions.list") {
				break
			}
		}
		cancel()
		<-done
		if stored && (len(calls) == 0 || calls[0] != "engine.use") {
			t.Errorf("stored: calls %v", calls)
		}
		if !stored && slices.Contains(calls, "engine.use") {
			t.Errorf("never chosen: calls %v", calls)
		}
		if !slices.Contains(calls, "sessions.list") {
			t.Errorf("never listed: %v", calls)
		}
	}
}

// Orca is an engine the PUT takes like the others.
func TestEnginePutOrca(t *testing.T) {
	h, st, _, mux := engineHub(t)
	code, out := put(t, mux, "/api/v1/machines/m1/engine", `{"engine":"orca"}`)
	if code != 200 || out["engine"] != "orca" {
		t.Fatalf("PUT = %d %v", code, out)
	}
	var stored map[string]string
	if ok, _ := st.Setting(context.Background(), enginesKey, &stored); !ok || stored["m1"] != "orca" {
		t.Errorf("stored %v", stored)
	}
	if got := h.engineOf("m1"); got != "orca" {
		t.Errorf("engine %s", got)
	}
}
