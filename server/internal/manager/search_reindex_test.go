package manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistant"
)

// search-reindex finds the manager the way run does (--dir, else the only one here) and empties the index in the
// database its manager.json names. One without a database, or whose database is the hub's, is a configuration mistake
// and touches nothing.
func TestSearchReindexUsesTheManagersOwnDatabase(t *testing.T) {
	home := isolate(t)
	ctx := context.Background()
	var out strings.Builder
	e := newEnv(&out)
	var ce ConfigError
	if err := cmdSearchReindex(ctx, e, nil); !errors.As(err, &ce) || !strings.Contains(err.Error(), "--dir") {
		t.Fatalf("no manager here: %v", err)
	}
	dir := assistant.DefaultManagerDir("m-0a1b2c")
	c := assistant.ManagerConfig{ID: "m-0a1b2c", Hub: "http://[fd00::20]:18660", Token: "t"}
	if err := assistant.WriteManagerConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	if err := cmdSearchReindex(ctx, e, nil); !errors.As(err, &ce) || !strings.Contains(err.Error(), "no database yet") {
		t.Fatalf("no database: %v", err)
	}

	// manager.json's database: the one it attaches to (nothing listens there, and the error names it)
	c.DatabaseURL = "postgres://mgr:pw@127.0.0.1:1/persona_mgr_work?sslmode=disable&connect_timeout=2"
	if err := assistant.WriteManagerConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 3*time.Second) // not the 30 s a database coming up is given
	defer cancel()
	err := cmdSearchReindex(short, e, []string{"--dir", dir})
	if err == nil || errors.As(err, &ce) || !strings.Contains(err.Error(), "manager m-0a1b2c's database persona_mgr_work") {
		t.Fatalf("its own database: %v", err)
	}
	if strings.Contains(err.Error(), "pw@") {
		t.Fatalf("the error says the password: %v", err)
	}

	// the hub's database, as manager run refuses it
	hub := `{"database_url": "postgres://hub:pw@127.0.0.1:5432/persona?sslmode=disable"}`
	if err := os.WriteFile(filepath.Join(home, ".persona", "hub.json"), []byte(hub), 0o600); err != nil {
		t.Fatal(err)
	}
	c.DatabaseURL = "postgres://hub:pw@localhost/persona"
	if err := assistant.WriteManagerConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	if err := cmdSearchReindex(ctx, e, nil); !errors.As(err, &ce) || !errors.Is(err, assistant.ErrHubDatabase) {
		t.Fatalf("the hub's database: %v", err)
	}
	if err := cmdSearchReindex(ctx, e, []string{"extra"}); !errors.Is(err, errUsage) {
		t.Fatalf("an argument: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("said %q", out.String())
	}
}
