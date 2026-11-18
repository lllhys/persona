package main

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/link"
)

// Passive mode on the command line (docs/operations/link.md "Passive mode"): `pair --listen` makes the link the hub's admin page
// takes, `mode` switches between the two modes.

// pairListen is `persona-link pair --listen [addr] [--addr …]`: the link alone on stdout (for a pipe), the rest on stderr.
func pairListen(e *env, listen string, addrs []string) error {
	before, _ := link.Read(e.configPath())
	inv, err := link.PairListen(e.configPath(), listen, addrs, e.now())
	if err != nil {
		return err
	}
	fmt.Fprintln(e.stdout, inv.Link)
	w := e.stderr
	// Keep stdout a single canonical URL for pipes; baseline hubs can use the
	// alternate spelling on stderr. Both redeem the same one-time code.
	fmt.Fprintln(w, inv.LegacyLink)
	fmt.Fprintln(w, terminalText("link-cli.passive_mode_ready", i18n.Args{"listen": inv.Listen, "fingerprint": inv.FP}))
	fmt.Fprintln(w, terminalText("link-cli.passive_invite_hint", i18n.Args{"duration": duration(link.PairCodeLife)}))
	fmt.Fprintln(w, terminalText("link-cli.passive_invite_addresses", i18n.Args{"addresses": terminalJoin(inv.Addrs)}))
	if before != nil && before.Passive.Paired() {
		fmt.Fprintln(w, terminalText("link-cli.passive_replaces_hub", i18n.Args{"hub": passiveHubText(before.Passive)}))
	}
	fmt.Fprintln(w, firewallHint(e.goos, inv.Listen))
	switch {
	case link.Running(e.configPath()):
		fmt.Fprintln(w, terminalText("link-cli.passive_running"))
	case e.goos == "linux":
		fmt.Fprintln(w, terminalText("link-cli.passive_linux_next"))
	case e.goos == "windows":
		fmt.Fprintln(w, terminalText("link-cli.passive_windows_next"))
	case e.goos == "darwin":
		fmt.Fprintln(w, i18n.T(i18n.Terminal(), "link-service.passive_mac_next"))
	default:
		fmt.Fprintln(w, terminalText("link-cli.passive_other_next"))
	}
	return nil
}

// firewallHint says which port to open, and to whom.
func firewallHint(goos, listen string) string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		port = strings.TrimPrefix(listen, ":")
	}
	if goos == "windows" {
		return terminalText("link-cli.firewall_windows", i18n.Args{"port": port})
	}
	return terminalText("link-cli.firewall_other", i18n.Args{"port": port})
}

// passiveHubText names the hub passive mode is paired with.
func passiveHubText(p *link.Passive) string {
	switch {
	case p == nil:
		return ""
	case p.HubName != "":
		return terminalText("link-cli.passive_hub_name", i18n.Args{"name": p.HubName})
	case p.HubID != "":
		return " " + p.HubID
	}
	return ""
}

func modeName(mode string) string {
	if mode == link.ModePassive {
		return terminalText("link-cli.mode_passive")
	}
	return terminalText("link-cli.mode_active")
}

// modeCmd is `persona-link mode [active|passive] [--listen addr]`.
func modeCmd(e *env, args []string) error {
	var mode, listen string
	for i := 0; i < len(args); i++ {
		v, next, ok, err := flagValue(args, i, "--listen")
		switch {
		case err != nil:
			return err
		case ok:
			listen, i = v, next
		case mode == "" && (args[i] == link.ModeActive || args[i] == link.ModePassive):
			mode = args[i]
		default:
			return errUsage
		}
	}
	if mode == "" && listen == "" {
		c, _ := link.Read(e.configPath())
		cur := c.CurrentMode()
		fmt.Fprintln(e.stdout, terminalText("link-cli.mode_current", i18n.Args{"mode": modeName(cur)}))
		if cur == link.ModePassive {
			fmt.Fprintln(e.stdout, terminalText("link-cli.mode_listen", i18n.Args{"listen": c.Passive.ListenAddr()}))
		}
		return nil
	}
	if mode == "" {
		c, _ := link.Read(e.configPath())
		mode = c.CurrentMode()
	}
	c, err := link.SetMode(e.configPath(), mode, listen)
	if err != nil {
		return err
	}
	if mode == link.ModePassive {
		fmt.Fprintln(e.stdout, terminalText("link-cli.mode_changed_passive", i18n.Args{"mode": modeName(mode), "listen": c.Passive.ListenAddr()}))
	} else {
		fmt.Fprintln(e.stdout, terminalText("link-cli.mode_changed_active", i18n.Args{"mode": modeName(mode)}))
	}
	switch {
	case mode == link.ModePassive && !c.Passive.Paired():
		fmt.Fprintln(e.stdout, terminalText("link-cli.mode_passive_unpaired"))
	case mode == link.ModeActive && c.Token == "":
		fmt.Fprintln(e.stdout, terminalText("link-cli.mode_active_unpaired"))
	}
	if mode == link.ModePassive {
		fmt.Fprintln(e.stdout, firewallHint(e.goos, c.Passive.ListenAddr()))
	}
	if link.Running(e.configPath()) {
		fmt.Fprintln(e.stdout, terminalText("link-cli.mode_running"))
	}
	return nil
}

// ago says how long ago t was, for a person.
func ago(now, t time.Time) string {
	return terminalText("link-cli.ago", i18n.Args{"duration": duration(now.Sub(t))})
}
