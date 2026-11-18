package hub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/clitext"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/managerconf"
)

// The mesh from the command line on the hub's machine (docs/server/mesh.md "Admin APIs and CLI"): the same peers API as the admin page,
// under /api/v1/admin/cli/, over loopback with the master token (peerCLIRoutes).

// adminCallLimit bounds each of these commands' calls (adminCall): a hub that takes the connection but never answers (a
// stuck hub, or another program on hub.json's port) ends the command with an error instead of holding it forever. A
// variable so tests can shorten it.
var adminCallLimit = 45 * time.Second

// PairPeer prints a code another hub pairs with this one by (`personad hub pair --peer`): the code alone on stdout, what
// to do with it on stderr. ctx is the command's (Ctrl-C ends it).
func PairPeer(ctx context.Context) error {
	cfg, err := runningHubConfig()
	if err != nil {
		return err
	}
	var code peerCode
	if err := cfg.adminCall(ctx, http.MethodPost, "/api/v1/admin/cli/peers/code", nil, &code); err != nil {
		return err
	}
	fmt.Println(code.Code)
	fmt.Fprintln(os.Stderr, "\n"+i18n.T(i18n.Terminal(), "hub-cli.peer_code_hint", i18n.Args{
		"count":   max(1, int(time.Until(code.ExpiresAt).Round(time.Minute).Minutes())),
		"command": clitext.Named("personad hub peer add"),
	}))
	return nil
}

// PeerCommand is `personad hub peer add <code>|-`, `personad hub peer list`, `personad hub peer remove <hub_id>`,
// `personad hub peer center [<hub_id>] [--confirm]` (set as center; this hub when no id is given). ctx is the command's
// (Ctrl-C ends it); an argument mistake is a clitext.UsageError, found before hub.json is read. Help asked for prints
// the usage on out (exit 0).
func PeerCommand(ctx context.Context, args []string, stdin io.Reader, out io.Writer) error {
	if peerHelpAsked(args) {
		fmt.Fprintln(out, clitext.Named(peerUsage))
		return nil
	}
	if err := checkPeerArgs(args); err != nil {
		return err
	}
	cfg, err := runningHubConfig()
	if err != nil {
		return err
	}
	l := i18n.Terminal()
	switch args[0] {
	case "add":
		code := args[1]
		if code == "-" {
			if code, err = readPeerCode(ctx, l, stdin); err != nil {
				return err
			}
		}
		var p peerItem
		if err := cfg.adminCall(ctx, http.MethodPost, "/api/v1/admin/cli/peers", map[string]string{"code": code}, &p); err != nil {
			return err
		}
		fmt.Fprintln(out, i18n.T(l, "hub-cli.peer_added", i18n.Args{"name": clitext.Inert(p.Name), "id": clitext.Inert(p.HubID),
			"state": peerStateText(l, p)}))
		return nil
	case "list":
		var res struct {
			Peers      []peerItem      `json:"peers"`
			Discovered []discoveredHub `json:"discovered"`
		}
		if err := cfg.adminCall(ctx, http.MethodGet, "/api/v1/admin/cli/peers", nil, &res); err != nil {
			return err
		}
		var mesh struct {
			Role   string `json:"role"`
			Term   int64  `json:"term"`
			Center struct {
				HubID  string `json:"hub_id"`
				Name   string `json:"name"`
				Online bool   `json:"online"`
			} `json:"center"`
		}
		if err := cfg.adminCall(ctx, http.MethodGet, "/api/v1/admin/cli/mesh", nil, &mesh); err != nil && ctx.Err() != nil {
			return err // stopped (Ctrl-C): not an older hub without the mesh route
		} else if err == nil {
			if mesh.Role == "center" {
				fmt.Fprintln(out, i18n.T(l, "hub-cli.mesh_center", i18n.Args{"term": mesh.Term}))
			} else {
				state := i18n.T(l, "hub-cli.center_online")
				if !mesh.Center.Online {
					state = i18n.T(l, "hub-cli.center_offline")
				}
				fmt.Fprintln(out, i18n.T(l, "hub-cli.mesh_member", i18n.Args{"name": clitext.Inert(mesh.Center.Name),
					"id": clitext.Inert(mesh.Center.HubID), "state": state, "term": mesh.Term}))
			}
		}
		if len(res.Peers) == 0 {
			fmt.Fprintln(out, i18n.T(l, "hub-cli.peers_empty"))
		}
		// Names, ids and addresses are other hubs' text (a discovered one's is any LAN host's mDNS advert): inert, one
		// line per hub.
		for _, p := range res.Peers {
			role := p.Role
			switch p.Role {
			case "center":
				role = i18n.T(l, "hub-cli.role_center")
			case "member":
				role = i18n.T(l, "hub-cli.role_member")
			}
			fmt.Fprintln(out, i18n.T(l, "hub-cli.peer_line", i18n.Args{"id": clitext.Inert(p.HubID), "name": clitext.Inert(p.Name),
				"role": clitext.Inert(role), "state": peerStateText(l, p), "count": p.Machines}))
		}
		for _, d := range res.Discovered {
			fmt.Fprintln(out, i18n.T(l, "hub-cli.discovered", i18n.Args{"id": clitext.Inert(d.HubID), "name": clitext.Inert(d.Name),
				"addresses": clitext.Inert(strings.Join(d.Addresses, " "))}))
		}
		return nil
	case "remove":
		if err := cfg.adminCall(ctx, http.MethodDelete, "/api/v1/admin/cli/peers/"+args[1], nil, nil); err != nil {
			return err
		}
		fmt.Fprintln(out, i18n.T(l, "hub-cli.peer_removed"))
		return nil
	case "center":
		target, confirm := "", false
		for _, a := range args[1:] {
			if a == "--confirm" {
				confirm = true
			} else {
				target = a
			}
		}
		if target == "" {
			var health struct {
				HubID string `json:"hub_id"`
			}
			if err := cfg.adminCall(ctx, http.MethodGet, "/api/v1/health", nil, &health); err != nil {
				return err
			}
			target = health.HubID
		}
		var res struct {
			Term   int64 `json:"term"`
			Center struct {
				Name string `json:"name"`
			} `json:"center"`
		}
		err := cfg.adminCall(ctx, http.MethodPost, "/api/v1/admin/cli/peers/"+target+"/center", map[string]bool{"confirm": confirm}, &res)
		if err != nil {
			// The hub's code says the current center is unreachable (peer_admin.go), whatever language its text is in.
			if refused := (*adminCallError)(nil); errors.As(err, &refused) && refused.code == "confirm_required" {
				return &cliError{i18n.T(l, "hub-cli.center_confirm_hint", i18n.Args{"error": err}), err}
			}
			return err
		}
		fmt.Fprintln(out, i18n.T(l, "hub-cli.center_now", i18n.Args{"name": clitext.Inert(res.Center.Name), "term": res.Term}))
		return nil
	}
	return fmt.Errorf("unknown peer command %q", args[0]) // checkPeerArgs let through a command this switch lacks
}

// readPeerCode reads `peer add -`'s code, a line on stdin; Ctrl-C (ctx) ends the wait.
func readPeerCode(ctx context.Context, l i18n.Lang, stdin io.Reader) (string, error) {
	type read struct {
		line string
		err  error
	}
	got := make(chan read, 1)
	go func() { // left blocked on stdin when ctx ends first: the command is over then
		line, err := bufio.NewReader(stdin).ReadString('\n')
		got <- read{line, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-got:
		if r.err != nil && r.line == "" {
			return "", &cliError{i18n.T(l, "hub-cli.code_unreadable", i18n.Args{"error": r.err}), r.err}
		}
		return strings.TrimSpace(r.line), nil
	}
}

// peerUsage is `hub peer`'s usage line.
const peerUsage = "usage: personad hub peer add <code>|- | list | remove <hub_id> | center [<hub_id>] [--confirm]"

// peerHelpAsked: help as the command, or -h / --help anywhere (no code or hub id starts with "-": codes are
// persona://peer links, ids UUIDs).
func peerHelpAsked(args []string) bool {
	if len(args) > 0 && args[0] == "help" {
		return true
	}
	return slices.ContainsFunc(args, clitext.IsHelpArg)
}

// checkPeerArgs refuses what PeerCommand would otherwise drop or misread: an extra word (`list x`), a second id
// (`center a b`), a flag taken for an id or a code (`center --bogus`, `remove -x`).
func checkPeerArgs(args []string) error {
	if len(args) == 0 {
		return clitext.UsageError(peerUsage)
	}
	rest := args[1:]
	switch args[0] {
	case "add":
		if len(rest) != 1 || (rest[0] != "-" && strings.HasPrefix(rest[0], "-")) {
			return clitext.UsageError("usage: personad hub peer add <code> (- reads the code from stdin)")
		}
	case "list":
		if len(rest) != 0 {
			return clitext.UsageError("usage: personad hub peer list (takes no arguments)")
		}
	case "remove":
		if len(rest) != 1 || strings.HasPrefix(rest[0], "-") {
			return clitext.UsageError("usage: personad hub peer remove <hub_id>")
		}
	case "center":
		ids := 0
		for _, a := range rest {
			switch {
			case a == "--confirm":
			case strings.HasPrefix(a, "-"):
				return clitext.UsageError(fmt.Sprintf("peer center: unknown flag %q; usage: personad hub peer center [<hub_id>] [--confirm]", a))
			default:
				ids++
			}
		}
		if ids > 1 {
			return clitext.UsageError("usage: personad hub peer center [<hub_id>] [--confirm] (at most one hub id)")
		}
	default:
		return clitext.UsageError(fmt.Sprintf("unknown peer command %q; %s", args[0], peerUsage))
	}
	return nil
}

// peerStates are a peer link's states as the command line words them.
var peerStates = map[string]string{"connected": "hub-cli.state_connected", "connecting": "hub-cli.state_connecting",
	"offline": "hub-cli.state_offline", "refused": "hub-cli.state_refused"}

func peerStateText(l i18n.Lang, p peerItem) string {
	s := clitext.Inert(p.State)
	if key := peerStates[p.State]; key != "" {
		s = i18n.T(l, key)
	}
	if p.RTTMS != nil {
		s += i18n.T(l, "hub-cli.state_rtt", i18n.Args{"ms": *p.RTTMS})
	}
	if p.Error != "" {
		s += i18n.T(l, "hub-cli.state_error", i18n.Args{"error": clitext.Inert(p.Error)})
	}
	return s
}

// cliError is a command's error in the terminal's language, wrapping what it is about (errors.Is / As see err).
type cliError struct {
	text string
	err  error
}

func (e *cliError) Error() string { return e.text }
func (e *cliError) Unwrap() error { return e.err }

// adminCallError is the hub refusing an adminCall: its text (in the terminal's language, which the call asks for)
// and its apierr code, what a command checks instead of the text.
type adminCallError struct {
	status int
	code   string
	text   string
}

func (e *adminCallError) Error() string { return e.text }

// adminCall calls this machine's hub from loopback (managerconf.CLIHubURL, CLIDial) with the master token, for at most
// adminCallLimit. The hub answers in the terminal's language (i18n.Terminal, #5520), not in the user language an app
// last set.
func (c *Config) adminCall(ctx context.Context, method, path string, body, out any) error {
	l := i18n.Terminal()
	_, port, err := net.SplitHostPort(c.Listen)
	if n, perr := strconv.Atoi(port); err != nil || perr != nil || n <= 0 || n > 65535 {
		return errors.New(i18n.T(l, "hub-cli.listen_invalid"))
	}
	callCtx, cancel := context.WithTimeout(ctx, adminCallLimit)
	defer cancel()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(callCtx, method, managerconf.CLIHubURL(c.Listen)+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json") // the admin API takes JSON writes only
	req.Header.Set("Accept-Language", string(l))
	transport := &http.Transport{Proxy: nil, DialContext: managerconf.CLIDial}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err() // stopped (Ctrl-C)
		}
		if callCtx.Err() != nil {
			return fmt.Errorf("the hub at %s did not answer within %s: it may be stuck, or another program is listening on hub.json's port", req.URL.Host, adminCallLimit)
		}
		return errors.New(i18n.T(l, "hub-cli.hub_unreachable"))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		why := apierr.Describe(b) // with the cause the hub moved to detail (#5728)
		if why == "" {
			why = resp.Status
		}
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(b, &refusal)
		return &adminCallError{status: resp.StatusCode, code: refusal.Code,
			text: i18n.T(l, "hub-cli.hub_answered", i18n.Args{"status": resp.StatusCode, "error": why})}
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}
