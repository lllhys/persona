package node

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// A one-use grant is for the query on the confirmed card, not merely for the same method, path and body. Exercise the
// production path from a standalone command through larkCheck to the actual proxy and its credential-injecting upstream.
func TestGateGrantBindsConfirmedQuery(t *testing.T) {
	r := newSpaceRig(t)
	cwd := t.TempDir()
	base := r.processIn(cwd, "s1")
	const body = `{"receive_id":"ou_example","msg_type":"text","content":"{\"text\":\"hello\"}"}`
	const confirmedQuery = "receive_id_type=open_id"
	command := `ex POST '/im/v1/messages?` + confirmedQuery + `' '` + body + `'`
	if decision := r.check(cwd, "s1", command); decision != "allow" {
		t.Fatalf("larkCheck = %q, want allow", decision)
	}
	if card := r.hub.lastCreated(t); !strings.Contains(card.Summary, "?"+confirmedQuery) {
		t.Fatalf("confirmation card does not show the confirmed query: %q", card.Summary)
	}

	url := base + "/api/v1/im/v1/messages"
	if code, response := send(t, http.MethodPost, url+"?receive_id_type=user_id", body); code != http.StatusForbidden {
		t.Errorf("changed receive_id_type = %d %s, want 403", code, response)
	}
	if got := r.api.requests(); len(got) != 0 {
		t.Errorf("changed query reached upstream: %+v", got)
	}
	if code, response := send(t, http.MethodPost, url+"?"+confirmedQuery, body); code != http.StatusOK {
		t.Errorf("confirmed query after the changed request = %d %s, want 200", code, response)
	}
	got := r.api.requests()
	if len(got) != 1 || got[0].Query != confirmedQuery || got[0].Auth != "Bearer test-token" {
		t.Errorf("upstream requests = %+v, want the confirmed query once with the gate credential", got)
	}
	if code, _ := send(t, http.MethodPost, url+"?"+confirmedQuery, body); code != http.StatusForbidden {
		t.Errorf("repeated confirmed request = %d, want 403", code)
	}
	if got := r.api.requests(); len(got) != 1 {
		t.Errorf("repeated confirmed request reached upstream: %+v", got)
	}
	if cards := r.cards(); cards != 1 {
		t.Errorf("proxy requests created cards: got %d, want only larkCheck's card", cards)
	}
}

func TestGateGrantQueryMatching(t *testing.T) {
	const body = `{"text":"n"}`
	tests := []struct {
		name      string
		confirmed string
		sent      string
		match     bool
	}{
		{name: "added parameter differs", confirmed: "a=1", sent: "a=1&b=2"},
		{name: "missing query differs", confirmed: "a=1", sent: ""},
		{name: "changed value differs", confirmed: "a=1", sent: "a=2"},
		{name: "different key order is equivalent", confirmed: "b=2&a=1", sent: "a=1&b=2", match: true},
		{name: "repeated value order remains significant", confirmed: "a=1&a=2", sent: "a=2&a=1"},
		{name: "malformed queries with equal partial values still differ", confirmed: "a=1;b=2&c=3", sent: "a=1;b=changed&c=3"},
		{name: "identical malformed query matches exactly", confirmed: "a=1;b=2&c=3", sent: "a=1;b=2&c=3", match: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newSpaceRig(t)
			cwd := t.TempDir()
			base := r.processIn(cwd, "s1")
			command := fmt.Sprintf("ex POST '/notes/N1?%s' '%s'", tc.confirmed, body)
			if decision := r.check(cwd, "s1", command); decision != "allow" {
				t.Fatalf("larkCheck = %q, want allow", decision)
			}
			url := base + "/api/v1/notes/N1"
			code, response := send(t, http.MethodPost, url+"?"+tc.sent, body)
			if tc.match {
				if code != http.StatusOK {
					t.Errorf("equivalent query = %d %s, want 200", code, response)
				}
			} else {
				if code != http.StatusForbidden {
					t.Errorf("changed query = %d %s, want 403", code, response)
				}
				if got := r.api.requests(); len(got) != 0 {
					t.Errorf("changed query reached upstream: %+v", got)
				}
				code, response = send(t, http.MethodPost, url+"?"+tc.confirmed, body)
				if code != http.StatusOK {
					t.Errorf("confirmed query after mismatch = %d %s, want 200", code, response)
				}
			}
			if code, _ := send(t, http.MethodPost, url+"?"+tc.confirmed, body); code != http.StatusForbidden {
				t.Errorf("repeat after confirmed query = %d, want 403", code)
			}
			got := r.api.requests()
			if len(got) != 1 {
				t.Fatalf("upstream requests = %+v, want exactly one", got)
			}
			wantQuery := tc.confirmed
			if tc.match {
				wantQuery = tc.sent
			}
			if got[0].Query != wantQuery {
				t.Errorf("upstream query = %q, want the original forwarded bytes %q", got[0].Query, wantQuery)
			}
		})
	}
}

func TestGateAllowedGETsDoNotNeedQueryGrants(t *testing.T) {
	r := newSpaceRig(t)
	base := r.processIn(t.TempDir(), "s1")
	queries := []string{"b=2&a=1", "a=1&b=2", "b=2&a=1", "a=1&b=2"}
	for _, query := range queries {
		if code, response := send(t, http.MethodGet, base+"/api/v1/me?"+query, ""); code != http.StatusOK {
			t.Errorf("GET ?%s = %d %s, want 200", query, code, response)
		}
	}
	got := r.api.requests()
	if len(got) != len(queries) {
		t.Fatalf("upstream requests = %+v, want %d allowed GETs", got, len(queries))
	}
	for i, query := range queries {
		if got[i].Query != query {
			t.Errorf("GET %d upstream query = %q, want %q", i, got[i].Query, query)
		}
	}
	if cards := r.cards(); cards != 0 {
		t.Errorf("allowed GETs created %d cards, want zero", cards)
	}
	p := r.n.gates()
	p.mu.Lock()
	grants := len(p.grants)
	p.mu.Unlock()
	if grants != 0 {
		t.Errorf("allowed GETs created %d grants, want zero", grants)
	}
}
