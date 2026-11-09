package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Two runs at once get two databases, each usable, committing asynchronously and gone when its run ends; a database a
// killed run left behind long ago is swept by the next run, one of another kind is left alone.
func TestOwnDatabases(t *testing.T) {
	base := os.Getenv(Env)
	if base == "" {
		t.Skip(Env + " not set")
	}
	ctx := context.Background()
	a, doneA, err := Own(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	b, doneB, err := Own(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if a == b || a == base {
		t.Fatalf("not databases of their own: %s, %s", a, b)
	}
	for _, u := range []string{a, b} {
		c, err := pgx.Connect(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Exec(ctx, `CREATE TABLE t (x int); INSERT INTO t VALUES (1)`); err != nil {
			t.Fatal(err)
		}
		if sync := setting(t, c, "synchronous_commit"); sync != "off" {
			t.Fatalf("synchronous_commit %s", sync)
		}
		_ = c.Close(ctx)
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	// Closed after the drops below: cleanups run last first, and a defer would close it before any of them.
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	dropAfter := func(name string) {
		t.Cleanup(func() {
			if _, err := admin.Exec(context.Background(), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
				t.Errorf("drop %s: %v", name, err)
			}
		})
	}
	exists := func(u string) bool {
		pu, _ := url.Parse(u)
		var n int
		_ = admin.QueryRow(ctx, `SELECT count(*) FROM pg_database WHERE datname = $1`, strings.TrimPrefix(pu.Path, "/")).Scan(&n)
		return n == 1
	}
	doneA()
	if exists(a) || !exists(b) {
		t.Fatalf("after the first run: a %v, b %v", exists(a), exists(b))
	}
	doneB()
	if exists(b) {
		t.Fatal("the second database is still there")
	}

	// Names of this run's own: two runs of this test at once must not create the same database.
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	tag := hex.EncodeToString(buf)
	old := "persona_test_" + itoa(time.Now().Add(-7*time.Hour).Unix()) + "_" + tag
	other := "persona_test_sweep_" + tag
	oldManager := old + "_manager"     // the assistant's second database of a run that was killed
	oldTemplate := old + "_tpl_" + tag // a template of a run that was killed
	for _, n := range []string{old, oldManager, oldTemplate, other} {
		_, _ = admin.Exec(ctx, `DROP DATABASE IF EXISTS "`+n+`"`)
		if _, err := admin.Exec(ctx, `CREATE DATABASE "`+n+`"`); err != nil {
			t.Fatal(err)
		}
	}
	dropAfter(other)
	runURL, done, err := Own(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	// The assistant a test starts makes a second database next to the run's (assistant/owndb.go): done drops it too.
	runPath, _ := url.Parse(runURL)
	ownManager := "/" + strings.TrimPrefix(runPath.Path, "/") + "_manager"
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+strings.TrimPrefix(ownManager, "/")+`"`); err != nil {
		t.Fatal(err)
	}
	// A template the run migrated once is copied as it is, and each copy commits asynchronously too; done drops it.
	tpl, err := Template(ctx, runURL)
	if err != nil {
		t.Fatal(err)
	}
	c, err := pgx.Connect(ctx, tpl)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(ctx, `CREATE TABLE t (x int); INSERT INTO t VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	_ = c.Close(ctx)
	copied, err := Clone(ctx, runURL, databaseOf(tpl), "persona_test_clone_"+tag)
	if err != nil {
		t.Fatal(err)
	}
	dropAfter(databaseOf(copied))
	if c, err = pgx.Connect(ctx, copied); err != nil {
		t.Fatal(err)
	}
	var x int
	if err := c.QueryRow(ctx, `SELECT x FROM t`).Scan(&x); err != nil || x != 1 {
		t.Fatalf("the copy: %d %v", x, err)
	}
	if sync := setting(t, c, "synchronous_commit"); sync != "off" {
		t.Fatalf("the copy's synchronous_commit %s", sync)
	}
	_ = c.Close(ctx)
	done()
	if exists("/"+old) || exists("/"+oldManager) || exists("/"+oldTemplate) || !exists("/"+other) {
		t.Fatalf("sweep: old %v, old manager %v, old template %v, other %v", exists("/"+old), exists("/"+oldManager),
			exists("/"+oldTemplate), exists("/"+other))
	}
	if exists(ownManager) || exists(tpl) {
		t.Fatalf("the run's second database %v, its template %v", exists(ownManager), exists(tpl))
	}
}

func setting(t *testing.T, c *pgx.Conn, name string) string {
	t.Helper()
	var v string
	if err := c.QueryRow(context.Background(), `SELECT current_setting($1)`, name).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// A test binary started again by a test (a fake node) inherits its parent's own database and makes none of its own.
func TestOwnedBy(t *testing.T) {
	for u, want := range map[string]bool{
		"postgres://u:p@127.0.0.1:5432/persona_test_1793642187_12cf738f?sslmode=disable": true,
		"postgres://u:p@127.0.0.1:5432/persona_test?sslmode=disable":                     false,
		"postgres://u:p@127.0.0.1:5432/persona_test_deploy?sslmode=disable":              false,
	} {
		if got := ownedBy(u); got != want {
			t.Errorf("%s: %v", u, got)
		}
	}
}
