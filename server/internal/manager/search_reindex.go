package manager

import (
	"context"
	"errors"
	"flag"

	"github.com/nwverse/persona/server/internal/assistant"
)

// cmdSearchReindex is `personad manager search-reindex [--dir D]` (#6340): the chat search index of a manager made in
// the app emptied in its database (manager.json's database_url, as manager run uses it), so that it reads its chats
// again from the start (assistant.SearchReindex). The built-in manager's is `personad assistant search-reindex`.
func cmdSearchReindex(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("manager search-reindex", flag.ContinueOnError)
	fs.SetOutput(e.stdout)
	dir := fs.String("dir", "", "the manager's directory (may be left out when this computer has one manager)")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return errUsage
	}
	d := *dir
	if d == "" {
		if d, err = onlyManager(e.l()); err != nil {
			return ConfigError{err}
		}
	}
	c, err := assistant.ReadManagerConfig(d)
	if err != nil {
		return ConfigError{err}
	}
	if c.DatabaseURL == "" {
		return ConfigError{errors.New("this manager has no database yet (personad manager install makes it): no chat search index to empty")}
	}
	if hub := assistant.HubDatabaseURL(); hub != "" && assistant.SameDatabase(c.DatabaseURL, hub) {
		return ConfigError{assistant.ErrHubDatabase} // as manager run refuses it (ownDatabase)
	}
	return assistant.SearchReindex(ctx, c.DatabaseURL, "manager "+c.ID, e.stdout)
}
