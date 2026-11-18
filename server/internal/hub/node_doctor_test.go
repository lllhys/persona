package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func callDoctorRoute(h *Hub, role, method, path string) int {
	r := httptest.NewRequest(method, "/api/v1/machines/m1/"+path, nil)
	r.SetPathValue("mid", "m1")
	r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Role: role, ID: "dev"}))
	w := httptest.NewRecorder()
	method2 := "node.doctor"
	if strings.HasSuffix(path, "/fix") {
		method2 = "node.doctor.fix"
	}
	h.nodeDoctor(method2, 5e9)(w, r)
	return w.Code
}

// The report and the fix are for a full device: the others are refused before the node hears of it.
func TestNodeDoctorRoutesNeedAFullDevice(t *testing.T) {
	tn := newTermNode(t)
	h := tn.hub
	for _, role := range []string{roleAnswer, roleExtension, roleLink} {
		for _, tc := range [][2]string{{"GET", "doctor"}, {"POST", "doctor/fix"}} {
			if code := callDoctorRoute(h, role, tc[0], tc[1]); code != http.StatusForbidden {
				t.Errorf("%s %s: %s was let through: %d", tc[0], tc[1], role, code)
			}
		}
	}
	if got := tn.await(t, 0); len(got) != 0 {
		t.Fatalf("the node heard of a refused request: %q", got)
	}
	if code := callDoctorRoute(h, roleFull, "GET", "doctor"); code != http.StatusOK {
		t.Errorf("full device report = %d", code)
	}
	if code := callDoctorRoute(h, roleFull, "POST", "doctor/fix"); code != http.StatusOK {
		t.Errorf("full device fix = %d", code)
	}
	got := tn.await(t, 2)
	if len(got) != 2 || got[0] != "node.doctor" || got[1] != "node.doctor.fix" {
		t.Fatalf("node log = %q", got)
	}
	// The read never asks for a write, whatever the request says.
	r := httptest.NewRequest("GET", "/api/v1/machines/m1/doctor?fix=1", strings.NewReader(`{"fix":true}`))
	r.SetPathValue("mid", "m1")
	w := httptest.NewRecorder()
	h.nodeDoctor("node.doctor", 5e9)(w, r)
	tn.await(t, 3)
	tn.mu.Lock()
	args := tn.args["node.doctor"]
	tn.mu.Unlock()
	if args != `{}` {
		t.Errorf("the node was sent %s", args)
	}
}

// Not on the answer-only device's lists, and credentials-only in the extension scopes.
func TestNodeDoctorRouteForwardsOnlyExplicitPageLanguage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
		rpc    string
		header string
		expect string
	}{
		{name: "GET without a header keeps legacy params", method: http.MethodGet, path: "doctor", rpc: "node.doctor", expect: `{}`},
		{name: "fix without a header keeps legacy params", method: http.MethodPost, path: "doctor/fix", rpc: "node.doctor.fix", expect: `{}`},
		{name: "GET English", method: http.MethodGet, path: "doctor", rpc: "node.doctor", header: "en-US,en;q=0.8", expect: `{"locale":"en"}`},
		{name: "fix Japanese", method: http.MethodPost, path: "doctor/fix", rpc: "node.doctor.fix", header: "ja", expect: `{"locale":"ja"}`},
		{name: "Chinese variant", method: http.MethodGet, path: "doctor", rpc: "node.doctor", header: "zh-TW", expect: `{"locale":"zh-Hans"}`},
		{name: "unknown falls back to English", method: http.MethodGet, path: "doctor", rpc: "node.doctor", header: "de", expect: `{"locale":"en"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tn := newTermNode(t)
			r := httptest.NewRequest(tc.method, "/api/v1/machines/m1/"+tc.path, nil)
			r.SetPathValue("mid", "m1")
			if tc.header != "" {
				r.Header.Set("Accept-Language", tc.header)
			}
			r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Role: roleFull, ID: "dev"}))
			w := httptest.NewRecorder()
			tn.hub.nodeDoctor(tc.rpc, 5e9)(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			tn.await(t, 1)
			tn.mu.Lock()
			args := tn.args[tc.rpc]
			tn.mu.Unlock()
			if args != tc.expect {
				t.Errorf("node params = %s, want %s", args, tc.expect)
			}
		})
	}
}

func TestNodeDoctorRouteRelaysAnOldNodesRawReport(t *testing.T) {
	const oldReport = `{"ok":false,"dir":"/old/node","checks":[{"name":"node.sock","status":"fail","state":"","detail":"不存在","hint":"没有 node 在监听","items":[]}],"fixed":[]}`
	tn := newTermNode(t)
	tn.answers = map[string]any{"node.doctor": json.RawMessage(oldReport)}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/machines/m1/doctor", nil)
	r.SetPathValue("mid", "m1")
	r.Header.Set("Accept-Language", "en")
	r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Role: roleFull, ID: "dev"}))
	w := httptest.NewRecorder()
	tn.hub.nodeDoctor("node.doctor", 5e9)(w, r)
	if w.Code != http.StatusOK || w.Body.String() != oldReport {
		t.Fatalf("status %d, report = %s", w.Code, w.Body.String())
	}
	tn.await(t, 1)
	tn.mu.Lock()
	args := tn.args["node.doctor"]
	tn.mu.Unlock()
	if args != `{"locale":"en"}` {
		t.Errorf("node params = %s", args)
	}
	var legacy struct {
		Fix       bool  `json:"fix"`
		Preflight *bool `json:"preflight"`
	}
	if err := json.Unmarshal([]byte(args), &legacy); err != nil || legacy.Fix || legacy.Preflight != nil {
		t.Errorf("an old node cannot ignore the additive locale in %s: %v", args, err)
	}
}

func TestNodeDoctorRoutesAreFullCredentialsOnly(t *testing.T) {
	data, err := os.ReadFile("testdata/route_scopes.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range [][2]string{{http.MethodGet, "doctor"}, {http.MethodPost, "doctor/fix"}} {
		url := "/api/v1/machines/m1/" + tc[1]
		for _, p := range []principal{{ID: "dev_watch", Role: roleAnswer}, {ID: "dev_link", Role: roleLink}} {
			if allowed(p, tc[0], url) {
				t.Errorf("%s may %s %s", p.Role, tc[0], url)
			}
		}
		line := "-                    " + tc[0] + " /api/v1/machines/{mid}/" + tc[1] + "\n"
		if !strings.Contains(string(data), line) {
			t.Errorf("route_scopes.txt has no full-credentials-only line for %s %s", tc[0], tc[1])
		}
	}
}
