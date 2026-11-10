package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/errreport"
	"github.com/nwverse/persona/server/internal/extension"
	pserve "github.com/nwverse/persona/server/internal/extension/serve"
	"github.com/nwverse/persona/server/internal/extwire"
)

// serveErrorsExtensionForTest takes error reports and writes each delivery as a line of JSON to ERRORS_OUT.
func serveErrorsExtensionForTest() {
	var mu sync.Mutex
	out := os.Getenv("ERRORS_OUT")
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "example-errors", Version: "1.0.0",
		Namespaces: map[string]pserve.Handler{
			"error_reports": func(_ context.Context, method string, params json.RawMessage) (any, error) {
				if method != "deliver" {
					return nil, pserve.UnknownMethod(method)
				}
				mu.Lock()
				defer mu.Unlock()
				f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
				if err == nil {
					_, _ = f.Write(append(append([]byte{}, params...), '\n'))
					_ = f.Close()
				}
				return nil, nil
			}}})
}

func errorsPackage(t *testing.T, out string) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin", "errors"), []byte("#!/bin/sh\nPERSONA_TEST_PLUGIN=errors exec "+self+" \"$@\"\n"), 0o755)
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "example-errors", "name": "Example Errors", "version": "1.0.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": {%q: "bin/errors"}, "env": {"ERRORS_OUT": %q}},
		"activation_events": ["on_startup"],
		"contributes": {"error_reports": [{"id": "all"}, {"id": "apps", "sources": ["web", "android"]}]},
		"permissions": {"error_reports": true}}`, runtime.GOOS+"-"+runtime.GOARCH, out)
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644)
	return dir
}

// deliveries reads what the extension was given so far.
func deliveries(t *testing.T, out string) []wire.ErrorReportsDeliver {
	t.Helper()
	raw, _ := os.ReadFile(out)
	var got []wire.ErrorReportsDeliver
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if l == "" {
			continue
		}
		var d wire.ErrorReportsDeliver
		if err := json.Unmarshal([]byte(l), &d); err != nil {
			t.Fatalf("%q: %v", l, err)
		}
		got = append(got, d)
	}
	return got
}

// Error reports: off (and refused) with no extension taking them; with one, the apps' and the hub's reports are
// scrubbed, deduplicated and given to each contribution the sources it takes; the permission is shown first.
func TestErrorReports(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	m := &Machine{cfg: MachineConfig{ID: "box1", Name: "studio-mini"}}
	h.machines, h.byID["box1"] = append(h.machines, m), m

	report := `{"reports": [{"id": "r1", "source": "web", "kind": "exception", "level": "error", "time": "2026-11-06T09:00:00Z",
		"message": "send \"hello my secret plan\" failed at https://hub.example.com/api/v1/inbox?token=abc123 from 203.0.113.7 on studio-mini",
		"exceptions": [{"type": "TypeError", "value": "x is undefined",
			"frames": [{"function": "load", "file": "/Users/alice/src/app.js", "line": 3, "in_app": true}]}],
		"breadcrumbs": [{"category": "fetch", "message": "GET /api/v1/inbox Authorization: Bearer test-example-token"}],
		"fingerprint": "made-up", "count": 99}]}`
	if code, out := call(t, mux, "POST", "/api/v1/error-reports", master, report); code != 200 || out["enabled"] != false {
		t.Fatalf("no extension takes them: off, nothing taken: %d %v", code, out)
	}
	if h.ErrorReportsOn() || containsStr(h.features(), errorReportsFeature) {
		t.Fatal("no feature without an extension")
	}

	out := filepath.Join(t.TempDir(), "got.jsonl")
	_, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+errorsPackage(t, out)+`"}`)
	if !strings.Contains(fmt.Sprint(pv["permissions"]), "error_reports") {
		t.Fatalf("the permission is shown before installing: %v", pv["permissions"])
	}
	call(t, mux, "POST", "/api/v1/plugins/-/install", master, fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"]))
	waitLong(t, "the extension up", func() bool {
		hp := h.extensions.get("example-errors")
		return hp != nil && hp.proc != nil && hp.proc.Ready()
	})
	if !containsStr(h.features(), errorReportsFeature) {
		t.Fatalf("the feature with an extension: %v", h.features())
	}

	if code, out := call(t, mux, "POST", "/api/v1/error-reports", master, report); code != 202 || out["accepted"] != float64(1) {
		t.Fatalf("%d %v", code, out)
	}
	if code, out := call(t, mux, "POST", "/api/v1/error-reports", master, report); code != 202 || out["accepted"] != float64(0) {
		t.Fatalf("the same id again is a retry: %d %v", code, out)
	}
	hub, _ := json.Marshal(extwire.ErrorReports{Reports: []wire.ErrorReport{
		{ID: "h1", Source: "hub", Kind: "log", Level: "error", Time: "2026-11-06T09:00:01Z", Message: "inbox: round failed: dial tcp 198.51.100.20:443: timeout"},
		{ID: "h2", Source: "web", Kind: "log", Level: "error", Time: "2026-11-06T09:00:01Z", Message: "the hub cannot speak for an app"}}})
	h.Event(extwire.EventErrorReports, hub)

	var got []wire.ErrorReportsDeliver
	waitLong(t, "the deliveries", func() bool {
		got = deliveries(t, out)
		n := 0
		for _, d := range got {
			n += len(d.Reports)
		}
		return n >= 3 // r1 to both contributions, h1 to all
	})
	byContribution := map[string][]wire.ErrorReport{}
	for _, d := range got {
		byContribution[d.Contribution] = append(byContribution[d.Contribution], d.Reports...)
	}
	if len(byContribution["all"]) != 2 || len(byContribution["apps"]) != 1 || byContribution["apps"][0].ID != "r1" {
		t.Fatalf("each contribution its sources; the hub may not say it is an app: %+v", byContribution)
	}
	var r wire.ErrorReport
	for _, x := range byContribution["all"] {
		if x.ID == "r1" {
			r = x
		}
	}
	text, _ := json.Marshal(r)
	for _, leak := range []string{"hello my secret plan", "hub.example.com", "abc123", "203.0.113.7", "studio-mini", "alice", "pext_0123456789abcdef", "made-up"} {
		if strings.Contains(string(text), leak) {
			t.Fatalf("%q left in: %s", leak, text)
		}
	}
	if r.Count != 1 || r.Fingerprint == "" || r.ReceivedAt == "" || !strings.Contains(r.Message, "https://<host>/api/v1/inbox") ||
		r.Exceptions[0].Frames[0].File != "~/src/app.js" {
		t.Fatalf("the manager's fields, a scrubbed message: %s", text)
	}
	for _, x := range byContribution["all"] {
		if x.ID == "h1" && (strings.Contains(x.Message, "198.51.100") || x.Source != "hub") {
			t.Fatalf("the hub's report, scrubbed: %+v", x)
		}
	}
}

// A report the gate let on that finds the queue full is not lost from the counts: the next one of its problem is
// not folded into it and carries it.
func TestErrorReportsFullQueueKeepsTheFold(t *testing.T) {
	h := &Manager{}
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	h.errReports.once.Do(func() {
		h.errReports.gate = errreport.NewGate(func() time.Time { return now })
		h.errReports.queue = make(chan wire.ErrorReport, 1)
	})
	h.errReports.queue <- wire.ErrorReport{ID: "filler"}
	mk := func(id string) wire.ErrorReport {
		return wire.ErrorReport{ID: id, Source: wire.ErrorSourceHub, Kind: "log", Message: "inbox: round failed"}
	}
	hub := []string{wire.ErrorSourceHub}
	if h.takeErrorReport(mk("a"), "hub", hub, nil) {
		t.Fatal("no room: not taken")
	}
	<-h.errReports.queue
	now = now.Add(time.Second)
	if !h.takeErrorReport(mk("b"), "hub", hub, nil) {
		t.Fatal("the next one of the problem is not folded into one never delivered")
	}
	if r := <-h.errReports.queue; r.ID != "b" || r.Count != 2 {
		t.Fatalf("it carries the dropped one: %s %d", r.ID, r.Count)
	}
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
