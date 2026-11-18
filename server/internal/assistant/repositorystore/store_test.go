package repositorystore

import (
	"context"
	"os"
	"testing"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/db/dbtest"
)

// TestMain gives this package an isolated database so concurrent server test lanes do not share state.
func TestMain(m *testing.M) { os.Exit(dbtest.Main(m)) }

func openExisting(t testing.TB, url string) *Store {
	t.Helper()
	database, err := db.Open(context.Background(), url, db.AllSets)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	return New(database)
}

func open(t testing.TB) *Store {
	t.Helper()
	url := os.Getenv(dbtest.Env)
	if url == "" {
		t.Skip(dbtest.Env + " not set")
	}
	d := openExisting(t, url)
	if err := d.TruncateForTest(context.Background()); err != nil {
		t.Fatal(err)
	}
	return d
}
