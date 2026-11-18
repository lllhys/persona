package extmgr

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// The owner allows a sandboxed extension specific hosts in the admin UI only (#5697): saved, audited, shown with the
// extension, given to the egress proxy at once, revoked the same way, and forgotten with the extension. A device's
// credential has no route to them.
func TestOwnerAllowsHosts(t *testing.T) {
	h, store := extensionTestHub(t)
	ps := h.extensions
	mux := h.routes()
	master := h.cfg.Token
	code, pv := call(t, mux, "POST", "/api/v1/extensions/-/preview", master, `{"source": "dir:`+boxedPackage(t)+`"}`)
	if code != 200 {
		t.Fatalf("preview: %d %v", code, pv)
	}
	if code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", master,
		fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"])); code != 200 {
		t.Fatalf("install: %d %v", code, out)
	}
	admin := adminRoutes(h)
	allow := func(host string) (int, map[string]any) {
		return adminCall(t, admin, "POST", "/api/v1/admin/extensions/boxed/egress/allow", fmt.Sprintf(`{"host": %q}`, host))
	}
	allowed := func(out map[string]any) []string {
		sb, _ := out["sandbox"].(map[string]any)
		list, _ := sb["owner_allowed"].([]any)
		var hosts []string
		for _, x := range list {
			m := x.(map[string]any)
			hosts = append(hosts, fmt.Sprint(m["host"])+":"+fmt.Sprint(m["port"]))
		}
		return hosts
	}

	code, out := allow("https://rss.example.org/feed")
	if code != 200 || strings.Join(allowed(out), ",") != "rss.example.org:<nil>" {
		t.Fatalf("allow: %d %v", code, out)
	}
	if code, out = allow("feeds.example.net:8443"); code != 200 || len(allowed(out)) != 2 {
		t.Fatalf("allow with a port: %d %v", code, out)
	}
	// Allowing it again changes nothing and writes no audit row.
	before := len(store.audits())
	if code, out = allow("rss.example.org"); code != 200 || len(allowed(out)) != 2 || len(store.audits()) != before {
		t.Fatalf("allow again: %d %v", code, out)
	}
	for _, bad := range []string{"*.example.org", "127.0.0.1:1200", "localhost", ""} {
		if code, out := allow(bad); code != 400 || out["code"] != "invalid_host" {
			t.Errorf("%q: %d %v", bad, code, out)
		}
	}
	// A device's credential (the phone) cannot reach them.
	if code, _ := call(t, mux, "POST", "/api/v1/admin/extensions/boxed/egress/allow", deviceToken("dev_phone", roleFull), `{"host": "evil.example.org"}`); code == 200 {
		t.Fatal("a device allowed a host")
	}

	// Saved, audited, and in the proxy's rule for the extension's next connection.
	var saved egressAllowSettings
	if _, err := store.Setting(context.Background(), extensionEgressAllowKey, &saved); err != nil || len(saved.Hosts["boxed"]) != 2 {
		t.Fatalf("saved %+v %v", saved, err)
	}
	var actions []string
	for _, a := range store.audits() {
		if a.plugin == "boxed" && strings.HasPrefix(a.action, "egress_") {
			actions = append(actions, a.action+" "+string(a.detail)+" "+a.actor)
		}
	}
	if len(actions) != 2 || !strings.Contains(actions[0], `"host":"rss.example.org"`) || !strings.Contains(actions[0], "admin") {
		t.Fatalf("audit %v", actions)
	}
	if targets := ps.ownerTargets("boxed"); len(targets) != 2 || targets[0].Host != "rss.example.org" || targets[0].Port != "" || !targets[0].HTTP {
		t.Fatalf("targets %+v", targets)
	}

	// Revoked: gone from the list, the setting and the rule, and audited.
	code, out = adminCall(t, admin, "DELETE", "/api/v1/admin/extensions/boxed/egress/allow?host="+url.QueryEscape("rss.example.org"), "")
	if code != 200 || strings.Join(allowed(out), ",") != "feeds.example.net:8443" {
		t.Fatalf("revoke: %d %v", code, out)
	}
	if last := store.audits()[len(store.audits())-1]; last.action != "egress_revoke" {
		t.Fatalf("revoke audit %+v", last)
	}

	// Uninstalled: forgotten, so an install later starts with none.
	if code, out := adminCall(t, admin, "DELETE", "/api/v1/admin/extensions/boxed", `{}`); code != 200 {
		t.Fatalf("uninstall: %d %v", code, out)
	}
	saved = egressAllowSettings{}
	_, _ = store.Setting(context.Background(), extensionEgressAllowKey, &saved)
	if len(saved.Hosts["boxed"]) != 0 || len(ps.ownerHosts("boxed")) != 0 {
		t.Fatalf("after uninstall %+v %v", saved, ps.ownerHosts("boxed"))
	}
}
