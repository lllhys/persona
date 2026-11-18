package assistant

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/managerconf"
)

// SearchReindexCommand is `personad assistant search-reindex` (#6340): the built-in manager's chat search index
// (search_index.go) emptied in the database it runs on, so that it reads its chats again from the start.
func SearchReindexCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("assistant search-reindex", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("assistant search-reindex takes no arguments")
	}
	cfg, err := readHubConfig()
	if err != nil {
		return err
	}
	url, err := builtinDatabaseNow(identity.HomeDir(), cfg.DatabaseURL)
	if err != nil {
		return err
	}
	return SearchReindex(ctx, url, "the built-in manager", stdout)
}

// builtinDatabaseNow is the database the built-in manager runs on, as its manager.json and hub.json say, making and
// moving nothing (builtinDatabase does that at its start): its own once its data moved there, else the hub's.
func builtinDatabaseNow(home, shared string) (string, error) {
	own := managerconf.ReadOwnDatabase(home)
	switch {
	case !own.Moved.IsZero() && own.URL != "": // a container's, with its own password
		return own.URL, nil
	case !own.Moved.IsZero() && own.Name == "":
		return "", errors.New("the built-in manager's manager.json says its data moved into its own database, but not where")
	case shared == "":
		return "", errors.New("no database_url in hub.json (or PERSONA_DATABASE_URL)")
	case !own.Moved.IsZero(): // a second database on the hub's Postgres server
		return db.WithDatabase(shared, own.Name)
	}
	return shared, nil // not moved (yet): its tables are in the hub's database
}

// SearchReindex empties the chat search index in a manager's database at url (db.ResetSearch), and says so on out.
// who names the manager. A manager that runs reads its chats again from the start at its next sweep (searchLoad),
// one that is stopped when it starts.
func SearchReindex(ctx context.Context, url, who string, out io.Writer) error {
	d, err := db.Attach(ctx, url, db.ManagerSet)
	if err != nil {
		return fmt.Errorf("%s's database %s: %w", who, db.DatabaseName(url), err)
	}
	defer d.Close()
	tables, err := d.Tables(ctx)
	if err != nil {
		return fmt.Errorf("%s's database %s: %w", who, db.DatabaseName(url), err)
	}
	if !slices.Contains(tables, "assistant_search_items") || !slices.Contains(tables, "assistant_search_cursors") {
		return fmt.Errorf("%s's database %s has no chat search index yet (it has not run a build with chat search): nothing to empty", who, db.DatabaseName(url))
	}
	if err := d.ResetSearch(ctx); err != nil {
		return fmt.Errorf("%s's database %s: %w", who, db.DatabaseName(url), err)
	}
	fmt.Fprintf(out, "Emptied the chat search index of %s (database %s). It reads its chats again from the start: "+
		"within about a minute when it runs, else when it starts. Until it is through, searches say they are still "+
		"going through older chats.\n", who, db.DatabaseName(url))
	return nil
}
