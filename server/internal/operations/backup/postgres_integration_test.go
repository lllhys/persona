package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/secretkey"
)

type postgresIntegration struct {
	hub, manager       Target
	hubURL, managerURL string
	backend            *postgresBackend
	ctx                context.Context
	previousNames      map[Target][]string
}

// newPostgresIntegration runs only against an explicitly provided disposable
// server. It never uses the default runtime directory or a database named by
// the real hub configuration; the databases it makes are dropped afterwards.
func newPostgresIntegration(t *testing.T) *postgresIntegration {
	t.Helper()
	base := os.Getenv("PERSONA_TEST_DATABASE_URL")
	container := os.Getenv("PERSONA_TEST_POSTGRES_CONTAINER")
	if base == "" {
		skip(t, "set PERSONA_TEST_DATABASE_URL and optionally PERSONA_TEST_POSTGRES_CONTAINER for disposable PostgreSQL verification")
	}
	if container == "" {
		if _, err := exec.LookPath("pg_dump"); err != nil {
			skip(t, "pg_dump is unavailable; set PERSONA_TEST_POSTGRES_CONTAINER")
		}
		if _, err := exec.LookPath("pg_restore"); err != nil {
			skip(t, "pg_restore is unavailable; set PERSONA_TEST_POSTGRES_CONTAINER")
		}
	}
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		fatal(t, err)
	}
	id := hex.EncodeToString(random)
	hubName := "pb_it_hub_" + id
	managerName := "pb_it_manager_" + id + "_" + strings.Repeat("m", 35)
	if len(managerName) > 63 {
		fatal(t, "test manager database name exceeds PostgreSQL's limit")
	}
	hubURL, err := withDatabase(base, hubName)
	if err != nil {
		fatal(t, err)
	}
	managerURL, err := withDatabase(base, managerName)
	if err != nil {
		fatal(t, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	it := &postgresIntegration{
		hub:     Target{Part: "hub", URL: hubURL, Database: hubName, Container: container, DefaultContainer: container},
		manager: Target{Part: "manager", URL: managerURL, Database: managerName, Container: container, DefaultContainer: container},
		hubURL:  hubURL, managerURL: managerURL, backend: newPostgresBackend(), ctx: ctx, previousNames: map[Target][]string{},
	}
	for _, target := range []Target{it.hub, it.manager} {
		if err := it.backend.Create(ctx, target, target.Database); err != nil {
			fatal(t, err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		for _, target := range []Target{it.hub, it.manager} {
			_ = it.backend.Drop(cleanupCtx, target, target.Database)
			for _, name := range it.previousNames[target] {
				_ = it.backend.Drop(cleanupCtx, target, name)
			}
		}
	})
	return it
}

func (it *postgresIntegration) exec(t *testing.T, target Target, statement string, args ...any) {
	t.Helper()
	conn, err := it.backend.connect(it.ctx, target, target.Database)
	if err != nil {
		fatal(t, err)
	}
	defer closeConn(it.ctx, conn)
	if _, err := conn.Exec(it.ctx, statement, args...); err != nil {
		fatal(t, err)
	}
}

// home writes a runtime directory naming the disposable databases.
func (it *postgresIntegration) home(t *testing.T, managerKey []byte) (string, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "runtime")
	writeFixtureFile(t, filepath.Join(home, "hub.json"), []byte(`{"database_url":"`+it.hubURL+`","token":"disposable-token"}`))
	writeFixtureFile(t, filepath.Join(home, "managers", "assistant", "manager.json"), []byte(`{"database_name":"`+it.manager.Database+`"}`))
	writeFixtureFile(t, filepath.Join(home, "secret.key"), []byte(strings.Repeat("h", secretkey.Size)))
	writeFixtureFile(t, filepath.Join(home, "managers", "assistant", "secret.key"), managerKey)
	writeFixtureFile(t, filepath.Join(home, "data", "fact.md"), []byte("disposable knowledge"))
	return home, filepath.Join(root, "out", "install.backup")
}

func TestPostgresWholeInstallRoundTrip(t *testing.T) {
	it := newPostgresIntegration(t)
	ctx, backend, hub, manager, hubURL, container := it.ctx, it.backend, it.hub, it.manager, it.hubURL, it.hub.Container
	previousNames := it.previousNames
	managerKey := []byte(strings.Repeat("k", secretkey.Size))
	for _, item := range []struct {
		target  Target
		part    string
		version int
	}{
		{hub, "hub", 1}, {manager, "manager", 430},
	} {
		conn, err := backend.connect(ctx, item.target, item.target.Database)
		if err != nil {
			fatal(t, err)
		}
		statements := []string{
			`CREATE TABLE schema_migrations (version integer, name text, part text)`,
			`INSERT INTO schema_migrations VALUES ($1, $2, $3)`,
			`CREATE TABLE backup_sample (id integer PRIMARY KEY, value text)`,
			`INSERT INTO backup_sample VALUES (1, 'original')`,
		}
		params := [][]any{{}, {item.version, "0001_test.sql", item.part}, {}, {}}
		if item.target.Part == "manager" {
			statements = append(statements, `CREATE TABLE sealing_key (fingerprint text)`, `INSERT INTO sealing_key VALUES ($1)`)
			params = append(params, []any{}, []any{secretkey.Fingerprint(managerKey)})
		}
		for index, statement := range statements {
			if _, err := conn.Exec(ctx, statement, params[index]...); err != nil {
				closeConn(ctx, conn)
				fatal(t, err)
			}
		}
		closeConn(ctx, conn)
	}

	home, output := it.home(t, managerKey)
	operator := &Operator{backend: backend, now: time.Now, random: rand.Reader}
	if _, err := operator.Create(ctx, Options{Home: home, Output: output, WritesQuiesced: true}); err != nil {
		fatal(t, err)
	}
	if _, err := operator.Verify(ctx, Options{Home: home, Bundle: output}); err != nil {
		fatal(t, err)
	}
	conn, err := backend.connect(ctx, hub, hub.Database)
	if err != nil {
		fatal(t, err)
	}
	if _, err := conn.Exec(ctx, `UPDATE backup_sample SET value = 'changed' WHERE id = 1`); err != nil {
		fatal(t, err)
	}
	closeConn(ctx, conn)
	writeFixtureFile(t, filepath.Join(home, "data", "fact.md"), []byte("changed knowledge"))
	result, err := operator.Restore(ctx, Options{Home: home, Bundle: output, WritesQuiesced: true})
	if err != nil {
		fatal(t, err)
	}
	previousNames[hub] = append(previousNames[hub], result.PreviousHubDB)
	previousNames[manager] = append(previousNames[manager], result.PreviousManagerDB)
	for _, item := range []struct {
		target Target
		want   string
	}{{hub, "original"}, {Target{Part: "hub", URL: hubURL, Database: result.PreviousHubDB, Container: container, DefaultContainer: container}, "changed"}} {
		conn, err := backend.connect(ctx, item.target, item.target.Database)
		if err != nil {
			fatal(t, err)
		}
		var got string
		if err := conn.QueryRow(ctx, `SELECT value FROM backup_sample WHERE id = 1`).Scan(&got); err != nil {
			closeConn(ctx, conn)
			fatal(t, err)
		}
		closeConn(ctx, conn)
		if got != item.want {
			errorf(t, "database %s value = %q, want %q", item.target.Database, got, item.want)
		}
	}
	raw, err := os.ReadFile(filepath.Join(home, "data", "fact.md"))
	if err != nil || string(raw) != "disposable knowledge" {
		errorf(t, "restored knowledge = %q, %v", raw, err)
	}
}

// TestPostgresUnrecordedManagerKey runs the sealed-value proof against a real
// server and the real migrations: a manager database before its first key
// rotation (sealing_key empty) with sealed values in a table and in list and
// object settings.
func TestPostgresUnrecordedManagerKey(t *testing.T) {
	it := newPostgresIntegration(t)
	managerKey := []byte(strings.Repeat("k", secretkey.Size))
	seal := func(plain string) string {
		sealed, err := secretkey.SealWith(managerKey, plain)
		if err != nil {
			fatal(t, err)
		}
		return sealed
	}
	// The real migrations, so the sealed-value query meets the real schema.
	for _, item := range []struct {
		target Target
		sets   db.Sets
	}{{it.hub, db.HubSet}, {it.manager, db.ManagerSet}} {
		migrated, err := db.Open(it.ctx, item.target.URL, item.sets)
		if err != nil {
			fatal(t, err)
		}
		migrated.Close()
	}
	it.exec(t, it.manager, `INSERT INTO connector_secrets (connector, sealed) VALUES ('example', $1)`, seal("connector token"))
	it.exec(t, it.manager, `INSERT INTO settings (key, value) VALUES ('launchers.managed', jsonb_build_array(jsonb_build_object('key_sealed', $1::text), 'not an object'))
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, seal("launcher key"))
	it.exec(t, it.manager, `INSERT INTO settings (key, value) VALUES ('bots.feishu', jsonb_build_object('secret_enc', $1::text))
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, seal("bot secret"))

	identity, err := it.backend.Identity(it.ctx, it.manager, "manager", it.manager.Database)
	if err != nil {
		fatal(t, err)
	}
	if identity.SealingKey != "" || len(identity.SealedValues) != 3 {
		fatalf(t, "unrecorded identity: key %q, %d sealed values, want 3", identity.SealingKey, len(identity.SealedValues))
	}

	home, output := it.home(t, managerKey)
	operator := &Operator{backend: it.backend, now: time.Now, random: rand.Reader}
	if _, err := operator.Create(it.ctx, Options{Home: home, Output: output, WritesQuiesced: true}); err != nil {
		fatal(t, err)
	}
	if _, err := operator.Verify(it.ctx, Options{Home: home, Bundle: output}); err != nil {
		fatal(t, err)
	}
	result, err := operator.Restore(it.ctx, Options{Home: home, Bundle: output, WritesQuiesced: true})
	if err != nil {
		fatal(t, err)
	}
	it.previousNames[it.hub] = append(it.previousNames[it.hub], result.PreviousHubDB)
	it.previousNames[it.manager] = append(it.previousNames[it.manager], result.PreviousManagerDB)

	writeFixtureFile(t, filepath.Join(home, "managers", "assistant", "secret.key"), []byte(strings.Repeat("w", secretkey.Size)))
	wrong := filepath.Join(filepath.Dir(output), "wrong-key.backup")
	_, err = operator.Create(it.ctx, Options{Home: home, Output: wrong, WritesQuiesced: true})
	if err == nil || !strings.Contains(err.Error(), "none of its 3 sealed secret(s)") {
		fatalf(t, "create with a key that opens no sealed value = %v", err)
	}
	if _, statErr := os.Stat(wrong); statErr == nil {
		fatal(t, "an unverifiable bundle was published")
	}
}
