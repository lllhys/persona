package backup

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// dumpAndRestore runs the real pg_dump and pg_restore through rawURL: a
// database is made, dumped, restored into a second one, and both dropped.
func dumpAndRestore(t *testing.T, rawURL string) {
	t.Helper()
	for _, program := range []string{"pg_dump", "pg_restore"} {
		if _, err := exec.LookPath(program); err != nil {
			skipf(t, "%s is unavailable", program)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id := randomSuffix(t)
	source, copyName := "pb_it_cli_"+id, "pb_it_cli_copy_"+id
	sourceURL, err := withDatabase(rawURL, source)
	if err != nil {
		fatal(t, err)
	}
	target, err := makeTarget("hub", sourceURL, "", "")
	if err != nil {
		fatal(t, err)
	}
	backend := newPostgresBackend()
	for _, name := range []string{source, copyName} {
		if err := backend.Create(ctx, target, name); err != nil {
			fatal(t, err)
		}
		t.Cleanup(func() { _ = backend.Drop(context.Background(), target, name) })
	}
	dump := filepath.Join(t.TempDir(), "source.dump")
	if err := backend.Dump(ctx, target, dump); err != nil {
		fatalf(t, "pg_dump: %v", err)
	}
	if err := backend.Restore(ctx, target, copyName, dump); err != nil {
		fatalf(t, "pg_restore: %v", err)
	}
}

// The first host of the URL refuses connections; pgx goes on to the next one,
// and so must pg_dump and pg_restore.
func TestPostgresClientsUseTheFallbackHost(t *testing.T) {
	base := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if base == "" {
		skip(t, "set PERSONA_TEST_DATABASE_URL to a disposable server reachable over TCP")
	}
	u, err := url.Parse(base)
	if err != nil || u.Port() == "" || strings.HasPrefix(u.Query().Get("host"), "/") {
		skip(t, "PERSONA_TEST_DATABASE_URL needs a TCP host:port authority")
	}
	u.Host = "127.0.0.1:1," + u.Host
	dumpAndRestore(t, u.String())
}

// PERSONA_TEST_TLS_DATABASE_URL connects with a client certificate whose key is
// encrypted (sslkey, sslcert, sslrootcert, sslpassword): the clients must open
// it without a prompt, as pgx does.
func TestPostgresClientsOpenAnEncryptedTLSKey(t *testing.T) {
	rawURL := os.Getenv("PERSONA_TEST_TLS_DATABASE_URL")
	if rawURL == "" {
		skip(t, "set PERSONA_TEST_TLS_DATABASE_URL to a disposable server URL with an encrypted client key and sslpassword")
	}
	dumpAndRestore(t, rawURL)
}
