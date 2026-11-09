package db

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nwverse/persona/server/internal/secretkey"
)

// Every column of the manager set that looks sealed (its name says so) is one the key rotation re-seals: a new sealed
// column goes into sealedTables, or the rotation would leave it sealed with a key it then deletes.
func TestSealedColumnsAreListed(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("migrations", "manager", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations: %v %v", files, err)
	}
	listed := SealedColumns()
	var missing []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range sealedLooking(string(b)) {
			if !slices.Contains(listed, c) {
				missing = append(missing, filepath.Base(f)+": "+c)
			}
		}
	}
	if len(missing) > 0 {
		t.Fatalf("sealed-looking columns the key rotation does not re-seal (add them to sealedTables): %v", missing)
	}
	// and every listed one is a column of the manager set's tables
	tables := ManagerTables()
	for _, c := range listed {
		if !slices.Contains(tables, strings.SplitN(c, ".", 2)[0]) {
			t.Errorf("%s: not a table of the manager set", c)
		}
	}
	// the scan sees both ways a column comes
	got := sealedLooking("-- a comment: x_enc text\nCREATE TABLE IF NOT EXISTS t (\n id text PRIMARY KEY,\n token_sealed text NOT NULL DEFAULT ''\n);\n" +
		"ALTER TABLE u ADD COLUMN IF NOT EXISTS secret_enc text, ADD COLUMN plain text;\nALTER TABLE v ADD COLUMN cipher_enc bytea, ADD COLUMN keys_sealed jsonb;")
	if want := []string{"t.token_sealed", "u.secret_enc", "v.cipher_enc", "v.keys_sealed"}; !slices.Equal(got, want) {
		t.Fatalf("scan %v, want %v", got, want)
	}
}

// sealedLooking are the table.column of the text, bytea, json and varchar columns in sql whose name says they hold a
// sealed value.
func sealedLooking(sql string) []string {
	create := regexp.MustCompile(`(?is)CREATE TABLE(?: IF NOT EXISTS)?\s+(\w+)\s*\((.*)\)`)
	alter := regexp.MustCompile(`(?is)ALTER TABLE(?: IF EXISTS)?\s+(\w+)\s+(.*)`)
	column := regexp.MustCompile(`(?im)^\s*(?:ADD COLUMN(?: IF NOT EXISTS)?\s+)?(\w+)\s+(?:text|bytea|jsonb?|varchar|character varying)\b`)
	sealedName := regexp.MustCompile(`(?i)(sealed|_enc$|^enc$|cipher)`)
	comment := regexp.MustCompile(`--[^\n]*`)
	var out []string
	for _, stmt := range strings.Split(comment.ReplaceAllString(sql, ""), ";") {
		var table, body string
		if m := create.FindStringSubmatch(stmt); m != nil {
			table, body = m[1], m[2]
		} else if m := alter.FindStringSubmatch(stmt); m != nil {
			table, body = m[1], strings.ReplaceAll(m[2], ",", "\n")
		} else {
			continue
		}
		for _, c := range column.FindAllStringSubmatch(body, -1) {
			if sealedName.MatchString(c[1]) {
				out = append(out, table+"."+c[1])
			}
		}
	}
	return out
}

// reseal is the rotation's step (internal/assistant resealWith) in small, for these tests.
func reseal(next []byte, olds ...[]byte) func(string) (string, ResealOutcome, error) {
	return func(v string) (string, ResealOutcome, error) {
		if _, err := secretkey.OpenWith(next, v); err == nil {
			return v, ResealKept, nil
		}
		for _, k := range olds {
			if p, err := secretkey.OpenWith(k, v); err == nil {
				s, err := secretkey.SealWith(next, p)
				return s, ResealDone, err
			}
		}
		return v, ResealUnreadable, nil
	}
}

func mustSeal(t *testing.T, key []byte, plain string) string {
	t.Helper()
	s, err := secretkey.SealWith(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustKey(t *testing.T) []byte {
	t.Helper()
	k, err := secretkey.Fresh()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// One transaction re-seals every listed place (both columns of a row together, the fields of the settings values),
// leaves unset and unreadable values as they are, keeps everything else in a settings value, and records the key.
func TestResealSecrets(t *testing.T) {
	url := migratedDatabase(t, ManagerSet)
	ctx := context.Background()
	d, err := Open(ctx, url, ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	old, next, other := mustKey(t), mustKey(t), mustKey(t)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := d.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO mail_accounts (id, email, data, password_enc, oauth_enc) VALUES ('a', 'a@example.com', '{}', $1, $2), ('b', 'b@example.com', '{}', '', $3)`,
		mustSeal(t, old, "pw-a"), mustSeal(t, old, "oauth-a"), mustSeal(t, other, "lost"))
	exec(`INSERT INTO connector_secrets (connector, sealed) VALUES ('telegram', $1)`, mustSeal(t, old, "session"))
	exec(`INSERT INTO plugins (id, source, version, manifest) VALUES ('p', 'builtin', '1', '{}')`)
	exec(`INSERT INTO plugin_secrets (plugin, name, sealed) VALUES ('p', 'key', $1), ('p', 'token', $2)`,
		mustSeal(t, old, "ext-key"), mustSeal(t, next, "already"))
	exec(`INSERT INTO plugin_markets (id, spec, token_sealed) VALUES ('m', 'github.com/example/market', $1), ('n', 'github.com/example/open', '')`,
		mustSeal(t, old, "market-token"))
	launchers, _ := json.Marshal([]map[string]any{
		{"name": "glm", "base_url": "https://api.example.com/<v1>", "key_sealed": mustSeal(t, old, "sk-1"), "headers_sealed": mustSeal(t, old, "X-A: b"), "created": "2026-11-01T00:00:00Z"},
		{"name": "plain", "base_url": "https://api.example.com"},
	})
	bot, _ := json.Marshal(map[string]any{"enabled": true, "app_id": "cli_x", "secret_enc": mustSeal(t, old, "bot-secret"), "nudge_after": 5})
	ms, _ := json.Marshal(map[string]any{"enc": mustSeal(t, old, "client-id")})
	stamp := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for k, v := range map[string][]byte{"launchers.managed": launchers, "bots.feishu": bot, "mail.microsoft_client": ms, "launchers.hidden": []byte(`["x"]`)} {
		exec(`INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, $3)`, k, v, stamp)
	}

	rep, err := d.ResealSecrets(ctx, reseal(next, old), "", &SealingKey{Fingerprint: secretkey.Fingerprint(next), Previous: secretkey.Fingerprint(old)})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Resealed != 9 || rep.Kept != 1 || len(rep.Unreadable) != 1 || rep.Unreadable[0] != "mail_accounts.oauth_enc" {
		t.Fatalf("report %+v", rep)
	}
	open := func(sql string, want string) {
		t.Helper()
		var v string
		if err := d.pool.QueryRow(ctx, sql).Scan(&v); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if p, err := secretkey.OpenWith(next, v); err != nil || p != want {
			t.Fatalf("%s: %q %v, want %q", sql, p, err, want)
		}
		if _, err := secretkey.OpenWith(old, v); err == nil {
			t.Fatalf("%s: still opens with the old key", sql)
		}
	}
	open(`SELECT password_enc FROM mail_accounts WHERE id = 'a'`, "pw-a")
	open(`SELECT oauth_enc FROM mail_accounts WHERE id = 'a'`, "oauth-a")
	open(`SELECT sealed FROM connector_secrets`, "session")
	open(`SELECT sealed FROM plugin_secrets WHERE name = 'key'`, "ext-key")
	open(`SELECT sealed FROM plugin_secrets WHERE name = 'token'`, "already")
	open(`SELECT token_sealed FROM plugin_markets WHERE id = 'm'`, "market-token")
	open(`SELECT value->0->>'key_sealed' FROM settings WHERE key = 'launchers.managed'`, "sk-1")
	open(`SELECT value->0->>'headers_sealed' FROM settings WHERE key = 'launchers.managed'`, "X-A: b")
	open(`SELECT value->>'secret_enc' FROM settings WHERE key = 'bots.feishu'`, "bot-secret")
	open(`SELECT value->>'enc' FROM settings WHERE key = 'mail.microsoft_client'`, "client-id")
	var lost, empty, base, created string
	var nudge int
	var at time.Time
	if err := d.pool.QueryRow(ctx, `SELECT (SELECT oauth_enc FROM mail_accounts WHERE id = 'b'), (SELECT password_enc FROM mail_accounts WHERE id = 'b'),
		(SELECT value->0->>'base_url' FROM settings WHERE key = 'launchers.managed'), (SELECT value->0->>'created' FROM settings WHERE key = 'launchers.managed'),
		(SELECT (value->>'nudge_after')::int FROM settings WHERE key = 'bots.feishu'), (SELECT updated_at FROM settings WHERE key = 'bots.feishu')`).
		Scan(&lost, &empty, &base, &created, &nudge, &at); err != nil {
		t.Fatal(err)
	}
	if p, err := secretkey.OpenWith(other, lost); err != nil || p != "lost" || empty != "" {
		t.Fatalf("an unreadable value was changed (%q %v) or an unset one set (%q)", p, err, empty)
	}
	if base != "https://api.example.com/<v1>" || created != "2026-11-01T00:00:00Z" || nudge != 5 || !at.Equal(stamp) {
		t.Fatalf("the rest of the settings changed: %q %q %d %v", base, created, nudge, at)
	}
	rec, err := d.SealingKey(ctx)
	if err != nil || rec == nil || rec.Fingerprint != secretkey.Fingerprint(next) || rec.Previous != secretkey.Fingerprint(old) || !rec.PreviousDropped.IsZero() {
		t.Fatalf("record %+v %v", rec, err)
	}

	// read without locks: every value, the settings' fields too ("" ones aside)
	vals, err := d.SealedValues(ctx)
	if err != nil || len(vals) != 11 {
		t.Fatalf("sealed values %d %v", len(vals), err)
	}
	// again: nothing to do, nothing written
	again, err := d.ResealSecrets(ctx, reseal(next, old), secretkey.Fingerprint(next), nil)
	if err != nil || again.Resealed != 0 || again.Kept != 10 {
		t.Fatalf("again: %+v %v", again, err)
	}
	if err := d.MarkPreviousDropped(ctx); err != nil {
		t.Fatal(err)
	}
	if rec, err := d.SealingKey(ctx); err != nil || rec.PreviousDropped.IsZero() {
		t.Fatalf("dropped not recorded: %+v %v", rec, err)
	}

	// a value of a shape it does not know stops the re-seal, which changes nothing then
	exec(`UPDATE settings SET value = '{"key_sealed": "x"}' WHERE key = 'launchers.managed'`)
	exec(`UPDATE connector_secrets SET sealed = $1`, mustSeal(t, old, "session-2"))
	if _, err := d.ResealSecrets(ctx, reseal(next, old), secretkey.Fingerprint(next), nil); err == nil {
		t.Fatal("a settings value of another shape was skipped")
	}
	exec(`UPDATE settings SET value = '[]' WHERE key = 'launchers.managed'`)
	// a record that is not what the re-seal expects (another process rotated meanwhile) stops it, changing nothing
	for _, expect := range []string{"", "another"} {
		if _, err := d.ResealSecrets(ctx, reseal(other, next), expect, &SealingKey{Fingerprint: "x"}); !errors.Is(err, ErrSealingKeyChanged) {
			t.Fatalf("expect %q: %v", expect, err)
		}
	}
	if rec, err := d.SealingKey(ctx); err != nil || rec.Fingerprint != secretkey.Fingerprint(next) || rec.Age < 0 || rec.Age > time.Minute {
		t.Fatalf("record after refused re-seals %+v %v", rec, err)
	}
	var still string
	if err := d.pool.QueryRow(ctx, `SELECT sealed FROM connector_secrets`).Scan(&still); err != nil {
		t.Fatal(err)
	}
	if _, err := secretkey.OpenWith(old, still); err != nil {
		t.Fatal("a failed re-seal changed a value")
	}
}

// Never in a database that has the hub's tables: one the hub shares, whose rows an older build opens with the hub's key.
func TestResealRefusesTheHubsDatabase(t *testing.T) {
	url := migratedDatabase(t, AllSets)
	ctx := context.Background()
	d, err := Open(ctx, url, AllSets)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	old := mustKey(t)
	sealed := mustSeal(t, old, "session")
	if _, err := d.pool.Exec(ctx, `INSERT INTO connector_secrets (connector, sealed) VALUES ('telegram', $1)`, sealed); err != nil {
		t.Fatal(err)
	}
	if hub, err := d.HasHubTables(ctx); err != nil || !hub {
		t.Fatalf("hub tables %v %v", hub, err)
	}
	if _, err := d.ResealSecrets(ctx, reseal(mustKey(t), old), "", &SealingKey{Fingerprint: "x"}); !errors.Is(err, ErrHubTables) {
		t.Fatalf("err %v", err)
	}
	var v string
	if err := d.pool.QueryRow(ctx, `SELECT sealed FROM connector_secrets`).Scan(&v); err != nil || v != sealed {
		t.Fatalf("changed: %v", err)
	}
	if rec, err := d.SealingKey(ctx); err != nil || rec != nil {
		t.Fatalf("recorded %+v %v", rec, err)
	}
}

// The lock is one process's at a time; OtherManagers counts a manager's connections that are not this process's.
func TestKeyRotationLock(t *testing.T) {
	url := migratedDatabase(t, ManagerSet)
	ctx := context.Background()
	a, err := Open(ctx, url, ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	la, err := a.LockKeyRotation(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() { // before a closes, which waits for the lock's connection
		if !released {
			la.Release()
		}
	}()
	if n, err := a.OtherManagers(ctx); err != nil || n != 0 {
		t.Fatalf("others %d %v", n, err)
	}
	b, err := Attach(ctx, url, ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if lb, err := b.LockKeyRotation(ctx, 300*time.Millisecond); !errors.Is(err, ErrKeyRotationBusy) {
		if lb != nil {
			lb.Release() // or b's Close waits for its connection
		}
		t.Fatalf("second lock: %v", err)
	}
	// b's pool keeps a connection open: another manager process
	if err := b.pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := a.OtherManagers(ctx); err != nil || n < 1 {
		t.Fatalf("others %d %v", n, err)
	}
	// a connection that is not a manager's is not counted
	c, err := pgx.Connect(ctx, url+"&application_name=persona-test")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	b.Close()
	n := -1
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if n, err = a.OtherManagers(ctx); err != nil || n == 0 { // the closed backends leave pg_stat_activity
			break
		}
	}
	if err != nil || n != 0 {
		t.Fatalf("others after b closed %d %v", n, err)
	}
	la.Release()
	released = true
	lb, err := a.LockKeyRotation(ctx, time.Second)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	lb.Release()
}

// After a commit that did not answer, SealingKeySettled waits for a re-seal still open on the server (its COMMIT on the
// way) and reads what it left; one open for longer than it waits is an error, never a "not committed".
func TestSealingKeySettled(t *testing.T) {
	url := migratedDatabase(t, ManagerSet)
	ctx := context.Background()
	d, err := Open(ctx, url, ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	open := func() (pgx.Tx, *pgx.Conn) {
		c, err := pgx.Connect(ctx, url+"&application_name=persona-test")
		if err != nil {
			t.Fatal(err)
		}
		tx, err := c.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `LOCK TABLE sealing_key IN EXCLUSIVE MODE`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sealing_key (fingerprint) VALUES ('inflight')`); err != nil {
			t.Fatal(err)
		}
		return tx, c
	}
	// held past the wait: no answer either way
	tx, c := open()
	if _, err := d.SealingKeySettled(ctx, 300*time.Millisecond); err == nil {
		t.Fatal("told an answer while the re-seal was still open")
	}
	_ = tx.Rollback(ctx)
	if rec, err := d.SealingKeySettled(ctx, time.Second); err != nil || rec != nil {
		t.Fatalf("after the rollback %+v %v", rec, err)
	}
	c.Close(ctx)
	// committed while it waits: it sees the commit
	tx, c = open()
	defer c.Close(ctx)
	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = tx.Commit(ctx)
	}()
	rec, err := d.SealingKeySettled(ctx, 10*time.Second)
	if err != nil || rec == nil || rec.Fingerprint != "inflight" {
		t.Fatalf("settled %+v %v", rec, err)
	}
}
