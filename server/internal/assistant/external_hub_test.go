package assistant

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/link"
)

// An assistant run elsewhere (assistant.external) pins a hub with its own self-signed certificate through
// PERSONA_ASSISTANT_FINGERPRINT, and still trusts the system's CAs without it (#1255).
func TestExternalHubPinsASelfSignedHub(t *testing.T) {
	hub := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hub"))
	}))
	defer hub.Close()
	t.Setenv("PERSONA_ASSISTANT_HUB", hub.URL)
	t.Setenv("PERSONA_ASSISTANT_FINGERPRINT", " "+link.Pin(hub.Certificate())+"\n")

	url, fp, err := externalHub("external", "127.0.0.1:18660")
	if err != nil || url != hub.URL || fp != link.Pin(hub.Certificate()) {
		t.Fatalf("externalHub = %q, %q, %v", url, fp, err)
	}
	// What run dials the hub with when the fingerprint is set (runOptions.Fingerprint).
	resp, err := pinnedHTTPClient(fp).Get(url)
	if err != nil {
		t.Fatalf("the pinned client does not reach the self-signed hub: %v", err)
	}
	resp.Body.Close()
	// And what it dials with otherwise: the system's CAs refuse the hub's own certificate.
	if resp, err := http.Get(url); err == nil {
		resp.Body.Close()
		t.Fatal("the system's CAs accepted a self-signed hub")
	}
	// A pin of another key is refused. (Every httptest server has the same certificate, so the other key is made up.)
	if resp, err := pinnedHTTPClient(strings.Repeat("A", 43)).Get(url); err == nil {
		resp.Body.Close()
		t.Fatal("the pinned client reached a hub with another key")
	}
}

func TestExternalHubWithoutAFingerprintTrustsTheSystem(t *testing.T) {
	t.Setenv("PERSONA_ASSISTANT_HUB", "https://hub.example.com:18662")
	t.Setenv("PERSONA_ASSISTANT_FINGERPRINT", "")
	url, fp, err := externalHub("external", "127.0.0.1:18660")
	if err != nil || url != "https://hub.example.com:18662" || fp != "" {
		t.Fatalf("externalHub = %q, %q, %v", url, fp, err)
	}
	t.Setenv("PERSONA_ASSISTANT_HUB", "")
	if url, fp, err = externalHub("external", "0.0.0.0:18670"); err != nil || url != "http://127.0.0.1:18670" || fp != "" {
		t.Fatalf("no hub set: externalHub = %q, %q, %v", url, fp, err)
	}
}

// The hub's own child dials the hub's local address and takes no pin, whatever the hub's environment holds.
func TestExternalHubChildTakesNoPin(t *testing.T) {
	t.Setenv("PERSONA_ASSISTANT_HUB", "http://127.0.0.1:18660")
	t.Setenv("PERSONA_ASSISTANT_FINGERPRINT", strings.Repeat("A", 43))
	if url, fp, err := externalHub("child", "127.0.0.1:18660"); err != nil || url != "http://127.0.0.1:18660" || fp != "" {
		t.Fatalf("externalHub = %q, %q, %v", url, fp, err)
	}
}

func TestExternalHubRefusesAPinItCannotUse(t *testing.T) {
	good := strings.Repeat("A", 43)
	for _, c := range []struct{ hub, fp, want string }{
		{"https://hub.example.com:18662", "sha256:" + good, "not a hub fingerprint"},
		{"https://hub.example.com:18662", good[:42], "not a hub fingerprint"},
		{"https://hub.example.com:18662", strings.Repeat("A", 42) + "=", "not a hub fingerprint"},
		{"http://hub.example.com:18660", good, "pins an https hub"},
		{"", good, "pins an https hub"}, // the local port is plain http
	} {
		t.Setenv("PERSONA_ASSISTANT_HUB", c.hub)
		t.Setenv("PERSONA_ASSISTANT_FINGERPRINT", c.fp)
		if _, _, err := externalHub("external", "127.0.0.1:18660"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("hub %q, fingerprint %q: err %v, want %q", c.hub, c.fp, err, c.want)
		}
	}
}
