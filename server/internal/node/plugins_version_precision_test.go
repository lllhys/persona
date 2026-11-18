package node

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginPruneArbitraryPrecisionVersionOrder(t *testing.T) {
	for _, tc := range []struct {
		name, current, rollback string
		older                   []string
	}{
		{
			name:     "core components",
			current:  "100000000000000000001.0.0",
			rollback: "100000000000000000000.0.0",
			older:    []string{"99999999999999999999.0.0", "100000000000000000000.0.0"},
		},
		{
			name:     "numeric prerelease components",
			current:  "1.0.0",
			rollback: "1.0.0-100000000000000000000",
			older:    []string{"1.0.0-99999999999999999999", "1.0.0-100000000000000000000"},
		},
		{
			name:     "numeric prerelease before alphanumeric",
			current:  "1.0.0",
			rollback: "1.0.0--1",
			older:    []string{"1.0.0-0", "1.0.0--1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ph := &pluginHost{dir: t.TempDir()}
			for _, version := range append([]string{tc.current}, tc.older...) {
				if err := os.MkdirAll(filepath.Join(ph.dir, "notes", version), 0o700); err != nil {
					t.Fatal(err)
				}
			}

			ph.prune("notes", tc.current)

			for _, version := range append([]string{tc.current}, tc.older...) {
				_, err := os.Stat(filepath.Join(ph.dir, "notes", version))
				want := version == tc.current || version == tc.rollback
				if (err == nil) != want {
					t.Errorf("version %s exists = %v, want %v (stat error: %v)", version, err == nil, want, err)
				}
			}
		})
	}
}
