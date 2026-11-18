package claudeplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
)

// Command is `claude-plugin <op>` with m: it runs op and prints the status as JSON. Exit code 0 when op was done, 1
// when it failed (the JSON says why), 2 for a usage error.
func Command(ctx context.Context, m *Manager, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || !slices.Contains(Ops, Op(args[0])) || Op(args[0]) == OpCheck {
		fmt.Fprintln(stderr, "usage: claude-plugin status|install|uninstall|on|off")
		return 2
	}
	st, err := m.Do(ctx, Op(args[0]))
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if eerr := enc.Encode(st); eerr != nil {
		fmt.Fprintln(stderr, eerr)
		return 1
	}
	if err != nil {
		return 1
	}
	return 0
}
