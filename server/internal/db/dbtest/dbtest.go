// Package dbtest gives a test binary a Postgres database of its own (#583). Every run of the server's tests used to
// point at the one PERSONA_TEST_DATABASE_URL database (persona_test): several lanes and a deploy testing on the hub
// machine at once truncated and migrated each other's tables mid-test, and tests failed with empty or foreign rows.
//
// A package with database tests calls Main from its TestMain: it creates persona_test_<time>_<random> next to the
// configured database, points PERSONA_TEST_DATABASE_URL at it for the tests (db.Open migrates it), and drops it
// afterwards. A role that may not create databases falls back to a lock: the runs then take turns on the shared one.
//
// The databases it makes commit asynchronously (synchronous_commit off): a commit is visible to other sessions at once,
// only a crash of the server can lose the last ones, which a throwaway database does not mind. A test migrates its
// database through a few hundred transactions, each of which would otherwise wait for a WAL flush.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/nwverse/persona/server/internal/identity"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nwverse/persona/server/internal/lazyre"
)

// Env names the database the tests use; without it they skip what needs one.
const Env = "PERSONA_TEST_DATABASE_URL"

// stale is how old a leftover database (a run that was killed) has to be before a later run drops it.
const stale = 6 * time.Hour

var ownName = lazyre.New(`^persona_test_(\d{10})_[0-9a-f]{8}$`)

// ownLeftover is a run's database or one made next to it: the second one the assistant made (owndb.go,
// "<database>_manager"; a test that starts the assistant with a state directory of its own gets one), or a template a
// test migrated once to copy (Template, "<database>_tpl_<random>"). Each outlives the run unless dropped.
var ownLeftover = lazyre.New(`^persona_test_(\d{10})_[0-9a-f]{8}(_manager|_tpl_[0-9a-f]{8})?$`)

// ownedBy says whether u is already a run's own database (set by a parent test binary).
func ownedBy(u string) bool {
	return ownName.MatchString(databaseOf(u))
}

// Main runs m on a database of its own and returns the exit code (os.Exit(dbtest.Main(m))).
func Main(m *testing.M) int {
	base := identity.Getenv(Env)
	if base == "" || ownedBy(base) {
		// A test binary a test started again (a fake node, a plugin) runs on its parent's database, which the parent
		// drops: its child is often killed before it could.
		return m.Run()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	own, done, err := Own(ctx, base)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: %v\n", err)
		return 1
	}
	defer done()
	_ = identity.Setenv(Env, own)
	return m.Run()
}

// Own makes a database for one run from base (a URL of a database the role can connect to) and returns its URL and
// how to drop it. When the role may not create databases, it is base itself, held by a lock until done.
func Own(ctx context.Context, base string) (string, func(), error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", nil, fmt.Errorf("parse %s: %w", Env, err)
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return "", nil, fmt.Errorf("connect: %w", err)
	}
	sweep(ctx, admin)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := fmt.Sprintf("persona_test_%d_%s", time.Now().Unix(), hex.EncodeToString(b))
	err = create(ctx, admin, name, "")
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "42501" { // insufficient_privilege: take turns on the shared database
		if _, err := admin.Exec(ctx, `SELECT pg_advisory_lock(hashtext('persona_test'))`); err != nil {
			_ = admin.Close(ctx)
			return "", nil, fmt.Errorf("lock: %w", err)
		}
		return base, func() { _ = admin.Close(context.Background()) }, nil // closing the session releases the lock
	}
	if err != nil {
		_ = admin.Close(ctx)
		return "", nil, fmt.Errorf("create database: %w", err)
	}
	own := *u
	own.Path = "/" + name
	return own.String(), func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		drop(ctx, admin, name+"_manager")
		for _, tpl := range templatesOf(ctx, admin, name) {
			drop(ctx, admin, tpl)
		}
		drop(ctx, admin, name)
		_ = admin.Close(ctx)
	}, nil
}

// create makes database name, a copy of template when that is not "", committing asynchronously (the package doc).
// A database copied keeps none of its template's settings, so each gets its own.
func create(ctx context.Context, admin *pgx.Conn, name, template string) error {
	q := `CREATE DATABASE ` + pgx.Identifier{name}.Sanitize()
	if template != "" {
		// WAL_LOG copies the template's pages through the WAL; FILE_COPY would take two checkpoints of the whole server.
		q += ` TEMPLATE ` + pgx.Identifier{template}.Sanitize() + ` STRATEGY wal_log`
	}
	if _, err := admin.Exec(ctx, q); err != nil {
		return err
	}
	if _, err := admin.Exec(ctx, `ALTER DATABASE `+pgx.Identifier{name}.Sanitize()+` SET synchronous_commit = off`); err != nil {
		drop(ctx, admin, name)
		return fmt.Errorf("asynchronous commit for %s: %w", name, err)
	}
	return nil
}

// Template makes an empty database next to own (the URL of a run's database, Main's) for a test to migrate once and
// copy with Clone, and returns its URL. The run's done drops it, and a later run sweeps it when the run was killed.
func Template(ctx context.Context, own string) (string, error) {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return Clone(ctx, own, "", databaseOf(own)+"_tpl_"+hex.EncodeToString(b))
}

// Clone makes database name next to own, a copy of database template ("": an empty one), and returns its URL; the caller
// drops it. Nothing may be connected to template: Postgres waits a few seconds for connections that are closing.
func Clone(ctx context.Context, own, template, name string) (string, error) {
	u, err := url.Parse(own)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", Env, err)
	}
	admin, err := pgx.Connect(ctx, own)
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	defer admin.Close(context.WithoutCancel(ctx))
	if err := create(ctx, admin, name, template); err != nil {
		return "", fmt.Errorf("create database %s: %w", name, err)
	}
	u.Path, u.RawPath = "/"+name, ""
	return u.String(), nil
}

// databaseOf is the database a URL names.
func databaseOf(u string) string {
	pu, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(pu.Path, "/")
}

// templatesOf are the templates made next to a run's database (Template).
func templatesOf(ctx context.Context, admin *pgx.Conn, name string) []string {
	rows, err := admin.Query(ctx, `SELECT datname FROM pg_database WHERE starts_with(datname, $1)`, name+"_tpl_")
	if err != nil {
		return nil
	}
	out, _ := pgx.CollectRows(rows, pgx.RowTo[string])
	return out
}

// drop removes a run's database, closing what is still connected to it (WITH FORCE, Postgres 13 and later).
func drop(ctx context.Context, admin *pgx.Conn, name string) {
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
		_, _ = admin.Exec(ctx, `DROP DATABASE IF EXISTS "`+name+`"`)
	}
}

// sweep drops the databases runs that were killed left behind, once they are old enough not to be in use.
func sweep(ctx context.Context, admin *pgx.Conn) {
	rows, err := admin.Query(ctx, `SELECT datname FROM pg_database WHERE datname LIKE 'persona\_test\_%'`)
	if err != nil {
		return
	}
	var old []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) != nil {
			continue
		}
		m := ownLeftover.FindStringSubmatch(name)
		if m == nil {
			continue // not one of ours (persona_test_deploy…)
		}
		if at, err := strconv.ParseInt(m[1], 10, 64); err == nil && time.Since(time.Unix(at, 0)) > stale {
			old = append(old, name)
		}
	}
	rows.Close()
	for _, name := range old {
		drop(ctx, admin, name)
	}
}
