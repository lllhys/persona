package identity

import "testing"

// DataSecretName is the one classification the path guard, the agent sandbox's lists and `hub doctor` share: known
// credential names by their start (backups included), or any base name ending in a key or credential suffix. It takes
// a base name: matching happens only at the top of a data directory, so a project's same-named file below it is left
// to its caller's depth check.
func TestDataSecretName(t *testing.T) {
	secret := []string{
		"hub.json", "hub.json.bak", "link.json", "tailcat.json", "tailcat-hub.json", "daemons.json",
		"manager.json", "manager.json.before-x", "apns.json", "apns_key.p8", "relay.json", "relay.secret",
		"relay-tunnel.token", "devices.json.migrated", "mail_accounts.json.1",
		"secret.key", "secret.key.bak", "handles.key~", "hub.key-old", "private.pem", "certificate.crt",
		"edge.token", "edge.token.bak", "edge.secret", "edge.password", "db-password.token",
	}
	for _, name := range secret {
		if !DataSecretName(name) {
			t.Errorf("%q: want a credential name", name)
		}
	}
	readable := []string{
		"mcp.json", "claude-settings.json", "notes.md",
		"notes.keywords", "notes.pemfile", "notes.p8data", "notes.crtinfo", // suffixes, not extensions
		"edge.tok", "edge.tokens", "edge.secrets", "edge.passwords", // near-miss extensions
		"db-password",     // protected because docker/ is a secret folder, not by its base name
		".hub.json.12345", // the dot-prefixed copy the sandbox alone adds
	}
	for _, name := range readable {
		if DataSecretName(name) {
			t.Errorf("%q: want an ordinary name", name)
		}
	}
}
