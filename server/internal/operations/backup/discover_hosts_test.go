package backup

import (
	"path/filepath"
	"strings"
	"testing"
)

// The hub and the built-in manager are the same database when they name the
// same database on any common server: pgx tries every host of a multi-host
// URL, so a shared fallback is enough to reach the same one.
func TestSameDatabaseComparesEveryHost(t *testing.T) {
	for _, c := range []struct {
		hub, manager string
		same         bool
	}{
		{"postgres://u@a,b/persona", "postgres://u@a,c/persona", true},
		{"postgres://u@b,a/persona", "postgres://u@c,a/persona", true},
		{"postgres://u@a:5432,b:5433/persona", "postgres://u@c:5432,B:5433/persona", true},
		{"postgres://u@a/persona", "postgres://u@a:5432/persona", true},
		{"postgres://u@/persona?host=/run/pg", "postgres://u@/persona?host=/tmp/other,/run/pg", true},
		{"postgres://u@a,b/persona", "postgres://u@c,d/persona", false},
		{"postgres://u@a:5432,b:5432/persona", "postgres://u@a:5433,b:5433/persona", false},
		{"postgres://u@a,b/persona", "postgres://u@a,b/persona_manager", false},
	} {
		hub := Target{URL: c.hub, Database: "persona"}
		manager := Target{URL: c.manager, Database: "persona"}
		if strings.HasSuffix(c.manager, "persona_manager") {
			manager.Database = "persona_manager"
		}
		if got := sameDatabase(hub, manager); got != c.same {
			errorf(t, "sameDatabase(%s, %s) = %v, want %v", c.hub, c.manager, got, c.same)
		}
	}
}

func TestDiscoverRefusesOverlappingMultiHostDatabases(t *testing.T) {
	home := filepath.Join(t.TempDir(), "runtime")
	writeFixtureFile(t, filepath.Join(home, "hub.json"), []byte(`{"database_url":"postgres://u@a,b/persona"}`))
	writeFixtureFile(t, filepath.Join(home, "managers", "assistant", "manager.json"), []byte(`{"database_url":"postgres://u@c,a/persona"}`))
	if _, _, err := Discover(Options{Home: home}); err == nil || !strings.Contains(err.Error(), "separate databases") {
		fatalf(t, "overlapping multi-host discovery = %v", err)
	}
}
