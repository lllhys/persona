package nodecmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nwverse/persona/server/internal/node"
	"github.com/nwverse/persona/server/internal/nodeclient"
)

// LaunchNodeWait is how long `personad launch` waits for the local node to hand over a launcher's settings. The node
// answers from memory, so a healthy one takes milliseconds (a Codex provider's at most node's codexCatalogWait more,
// while it looks for its gateway's model catalog); one that accepts the connection and never answers
// (restarting, stuck, or a stale socket held by another process) would otherwise keep the pane blank forever (#4548).
var LaunchNodeWait = 10 * time.Second

// runLaunch is `personad launch <launcher> [claude args…]`. stopSignals undoes main's SIGINT / SIGTERM handling
// first: launch has nothing to clean up (it execs claude right away), so Ctrl-C or a kill while it waits for the node
// should end it the way the process's inherited handling says, not sit in a context nobody reads.
func runLaunch(stopSignals func(), ticket, name string, args []string) error {
	stopSignals()
	ctx, cancel := context.WithTimeout(context.Background(), LaunchNodeWait)
	defer cancel()
	var err error
	if name == "--" { // `launch --ticket <t> -- claude …`: plain claude with a hand-off's values (launchTicket)
		err = node.LaunchWithSecrets(ctx, ticket, args)
	} else {
		err = node.Launch(ctx, name, args, ticket)
	}
	if err != nil && (errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)) {
		return fmt.Errorf("%s launch %s: the local node did not answer within %s (socket %s); "+
			"restart the Persona node on this computer, or run `%s node doctor` to see what is wrong",
			CommandName(), name, LaunchNodeWait, nodeclient.SocketPath(), CommandName())
	}
	return err
}

// launchTicket takes a leading `--ticket <ticket>` off args: the start's one-time hand-off (node/secretenv.go). A
// launcher's name never starts with "--", so without the flag args are as they were. "--" after the ticket stays,
// for runLaunch to tell plain claude from a launcher; without a ticket it is refused.
func launchTicket(args []string) (ticket string, rest []string, err error) {
	if len(args) == 0 || args[0] != "--ticket" {
		if len(args) > 0 && args[0] == "--" {
			return "", nil, errors.New("-- needs --ticket")
		}
		return "", args, nil
	}
	if len(args) < 3 || args[1] == "" {
		return "", nil, errors.New("--ticket needs a ticket and a launcher")
	}
	if args[2] == "--" && len(args) < 4 {
		return "", nil, errors.New("-- needs a command")
	}
	return args[1], args[2:], nil
}
