package modelhttp

import (
	"net/http"
	"testing"
)

func TestUserAgent(t *testing.T) {
	SetVersion("1.2.3")
	t.Cleanup(func() { SetVersion("dev") })
	if got := UserAgent(); got != "Persona/1.2.3" {
		t.Fatalf("UserAgent() = %q", got)
	}
	for _, custom := range []string{"custom-client", ""} {
		h := http.Header{}
		h.Set("User-Agent", custom)
		SetDefaultUserAgent(h)
		if got := h.Get("User-Agent"); got != custom {
			t.Errorf("custom User-Agent = %q, want %q", got, custom)
		}
	}
	// Forwarded JSON may carry a non-canonical key. net/http only recognizes the
	// canonical key when deciding whether to send its own default User-Agent.
	raw := http.Header{"user-agent": {"forwarded-client"}}
	SetDefaultUserAgent(raw)
	if got := raw.Get("User-Agent"); got != "forwarded-client" {
		t.Errorf("forwarded User-Agent = %q", got)
	}
	duplicate := http.Header{"User-Agent": {"canonical"}, "user-agent": {"duplicate"}}
	SetDefaultUserAgent(duplicate)
	if got := duplicate.Get("User-Agent"); got != "canonical" || len(duplicate) != 1 {
		t.Errorf("duplicate User-Agent headers = %v", duplicate)
	}
	h := http.Header{}
	SetDefaultUserAgent(h)
	if got := h.Get("User-Agent"); got != "Persona/1.2.3" {
		t.Errorf("default User-Agent = %q", got)
	}
}
