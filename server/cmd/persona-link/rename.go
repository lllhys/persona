package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/link"
)

// nameWait is how long `persona-link name <new>` waits for the hub's answer.
var nameWait = 20 * time.Second

// nameCmd is `persona-link name [<new>|--reset]`: what the hub calls this computer, or asks the running persona-link
// to have it call it new (docs/operations/link.md "The computer's name").
func nameCmd(e *env, args []string) error {
	path := e.configPath()
	switch {
	case len(args) == 0:
		c, err := link.Read(path)
		if errors.Is(err, os.ErrNotExist) {
			return link.NewTextError("link-cli.name_not_paired", nil)
		}
		if err != nil {
			return err
		}
		id, name := c.Machine, c.MachineName
		if c.CurrentMode() == link.ModePassive && c.Passive != nil {
			id, name = c.Passive.Machine, c.Passive.MachineName
		}
		if id == "" {
			return link.NewTextError("link-cli.name_not_paired", nil)
		}
		fmt.Fprintln(e.stdout, cmp.Or(name, id))
		if name != "" && name != id {
			fmt.Fprintln(e.stdout, terminalText("link-cli.name_id_stable", i18n.Args{"id": id}))
		}
		return nil
	case len(args) > 1:
		return errUsage
	}
	name := args[0]
	if name == "--reset" {
		name = ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), nameWait)
	defer cancel()
	got, err := link.RequestRename(ctx, path, name)
	var re *link.RenameError
	if errors.As(err, &re) {
		return errors.New(re.Text(i18n.Terminal()))
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(e.stdout, terminalText("link-cli.name_success", i18n.Args{"name": strings.TrimSpace(got)}))
	return nil
}
