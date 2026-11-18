package link

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/netaddr"
)

// Passive mode (docs/operations/link.md "Passive mode"): the hub dials this computer instead. persona-link listens on a TLS port with its
// own identity (tlsid.go) and offers three things there, nothing else: pairing, a little about itself, and streams
// to the resident node (listen.go). Pairing goes the other way round from active mode, as the computer may not reach
// the hub: `persona-link pair --listen` makes a one-time code and prints a link for the hub's admin page; the hub dials
// in, trades the code for a credential it made up, and opens streams with that credential from then on.

// Passive mode's endpoints and port.
const (
	ConnectorPairPath   = "/api/v1/connector/pair"
	ConnectorInfoPath   = "/api/v1/connector/info"
	ConnectorStreamPath = "/api/v1/connector/stream"
	DefaultListen       = ":18663"
)

// PingHeader is on a stream request from a hub whose hub.json sets the heartbeat (link): "<interval ms> <timeout ms>",
// how the hub pings the stream; the connector pings it the same (PingTimes). Older hubs send none and older connectors
// ignore it: each side keeps its own then.
const PingHeader = "X-Persona-Ping"

// MachineNameHeader is on a hub's info read (and its pairing request's machine_name says it too): what the hub calls
// this computer, URL query-escaped; the connector keeps it to show (docs/operations/link.md "The computer's name").
const MachineNameHeader = "X-Persona-Machine-Name"

// PingHeaderValue is PingHeader's value for a heartbeat.
func PingHeaderValue(every, timeout time.Duration) string {
	return strconv.FormatInt(every.Milliseconds(), 10) + " " + strconv.FormatInt(timeout.Milliseconds(), 10)
}

// pingFromHeader reads PingHeader's value; ok is false when it is missing or not two numbers that PingTimes takes.
func pingFromHeader(v string) (every, timeout time.Duration, ok bool) {
	f := strings.Fields(v)
	if len(f) != 2 {
		return 0, 0, false
	}
	e, err1 := strconv.ParseInt(f[0], 10, 64)
	t, err2 := strconv.ParseInt(f[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return PingTimesMS(e, t)
}

// PairCodeLife is how long a pairing code from `persona-link pair --listen` is good for (a setting).
const PairCodeLife = 10 * time.Minute

// Passive is passive mode's part of link.json.
type Passive struct {
	// Listen is where persona-link listens ("" means DefaultListen).
	Listen string `json:"listen,omitempty"`
	// The hub that paired, as it said: its id and name, and what it calls this computer.
	HubID   string `json:"hub_id,omitempty"`
	HubName string `json:"hub_name,omitempty"`
	Machine string `json:"machine,omitempty"`
	// MachineName is what that hub calls this computer, as it last said (pairing, its info reads); empty until a hub
	// that says it.
	MachineName string `json:"machine_name,omitempty"`
	// TokenSHA256 is the hex SHA-256 of the credential that hub made up: the credential itself is not kept here.
	TokenSHA256 string     `json:"token_sha256,omitempty"`
	PairedAt    *time.Time `json:"paired_at,omitempty"`
	// Code is the pairing code waiting for a hub, by its hash; used once, then gone.
	Code *PairCode `json:"code,omitempty"`
}

// PairCode is a one-time pairing code, by its hash, and until when it is good.
type PairCode struct {
	SHA256  string    `json:"sha256"`
	Expires time.Time `json:"expires"`
}

// Paired: a hub paired with this computer for passive mode.
func (p *Passive) Paired() bool { return p != nil && p.TokenSHA256 != "" }

// ListenAddr is where to listen.
func (p *Passive) ListenAddr() string {
	if p == nil || p.Listen == "" {
		return DefaultListen
	}
	return p.Listen
}

// CodeUntil is when the waiting pairing code runs out; zero when there is none, or it ran out.
func (p *Passive) CodeUntil(now time.Time) time.Time {
	if p == nil || p.Code == nil || !now.Before(p.Code.Expires) {
		return time.Time{}
	}
	return p.Code.Expires
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// sameHash compares a secret's hash with a kept one in constant time.
func sameHash(secret, kept string) bool {
	if kept == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hashHex(secret)), []byte(kept)) == 1
}

// newCode is a pairing code: 128 random bits.
func newCode() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "jc_" + base64.RawURLEncoding.EncodeToString(b)
}

// portNumber reads a port written as 1–5 ASCII digits, 0–65535. strconv.Atoi also takes a sign ("+443"), which
// net.Listen lets through but url.Parse and the hub's dialer refuse.
func portNumber(s string) (int, bool) {
	if len(s) == 0 || len(s) > 5 {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, n <= 65535
}

// ValidPort says whether s is a port another computer can dial: digits only, 1–65535.
func ValidPort(s string) bool {
	n, ok := portNumber(s)
	return ok && n >= 1
}

// validDialAddr says whether a (host:port) is an address the hub can dial: a host, a ValidPort, and a URL that parses.
func validDialAddr(a string) bool {
	host, port, err := net.SplitHostPort(a)
	if err != nil || host == "" || !ValidPort(port) {
		return false
	}
	_, err = url.Parse("https://" + a)
	return err == nil
}

// NormalizeListen checks a listen address and writes it the usual way: ":18663", "0.0.0.0:18663", "[::1]:18663". A
// bare port is taken for every address; empty is DefaultListen.
func NormalizeListen(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultListen, nil
	}
	if _, ok := portNumber(raw); ok {
		raw = ":" + raw
	}
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return "", newTextError("link-errors.listen_invalid", nil, i18n.Args{"address": strconv.Quote(raw)})
	}
	if _, ok := portNumber(port); !ok {
		return "", newTextError("link-errors.listen_port_invalid", nil, i18n.Args{"address": strconv.Quote(raw)})
	}
	if host != "" {
		ip, err := netip.ParseAddr(host)
		if err != nil || ip.Zone() != "" {
			return "", newTextError("link-errors.listen_host_invalid", nil, i18n.Args{"address": strconv.Quote(raw)})
		}
		host = ip.String()
	}
	return net.JoinHostPort(host, port), nil
}

// localAddrs lists this computer's addresses (tests replace it).
var localAddrs = netaddr.List

// ListenCandidates are the addresses the hub may reach a listener on listen at, best first by the rule active mode
// tries the hub's in (intranet → public IPv6 → public IPv4): this computer's addresses that are not loopback, link-local, on a
// virtual interface (Docker, VPN tunnels) or temporary IPv6, each with the port. Where every interface is virtual (a
// container's eth0, a NAS whose network card hangs off a virtual switch) the virtual ones count too, except tunnels and
// bridges. A listener on one address has just that one. Nothing here says the hub can reach them.
func ListenCandidates(listen string) ([]string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return nil, err
	}
	if host != "" {
		if ip, err := netip.ParseAddr(host); err == nil && !ip.IsUnspecified() {
			return []string{net.JoinHostPort(ip.String(), port)}, nil
		}
	}
	only4 := host == "0.0.0.0"
	addrs, err := localAddrs()
	if err != nil {
		return nil, err
	}
	out := candidates(addrs, port, only4, false)
	if len(out) == 0 {
		out = candidates(addrs, port, only4, true)
	}
	return out, nil
}

// tunnelOrBridge: interfaces whose addresses another computer cannot use to reach this one (by their usual names).
func tunnelOrBridge(name string) bool {
	name = strings.ToLower(name)
	// Keep the vendor prefix in display case, as netaddr.virtualName does: Go packs string constants together, so
	// unrelated neighboring text must not manufacture a configured machine name in the raw binary. Matching stays lowercase.
	for _, prefix := range []string{"lo", "docker", "veth", "br-", "virbr", "bridge", "tun", "tap", "utun", "tailscale", "wg", "zt",
		"ipsec", "vmnet", "VBoxNet", "awdl", "llw", "anpi", "gif", "stf"} {
		if strings.HasPrefix(name, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

func candidates(addrs []netaddr.Addr, port string, only4, virtual bool) []string {
	var eps []Endpoint
	for _, a := range addrs {
		ip := a.IP.Unmap().WithZone("")
		if (a.Virtual && (!virtual || tunnelOrBridge(a.Interface))) || a.InterfaceFlags&net.FlagUp == 0 ||
			a.InterfaceFlags&net.FlagLoopback != 0 || (only4 && !ip.Is4()) {
			continue
		}
		var kind string
		switch netaddr.Classify(ip) {
		case netaddr.PrivateIPv4, netaddr.ULA:
			kind = "lan"
		case netaddr.GlobalIPv6:
			if a.FlagsKnown && !netaddr.Stable(a) { // temporary, deprecated, tentative: gone soon
				continue
			}
			kind = "ipv6"
		case netaddr.PublicIPv4:
			kind = "ipv4"
		default:
			continue
		}
		eps = append(eps, Endpoint{URL: "https://" + net.JoinHostPort(ip.String(), port), Kind: kind})
	}
	var out []string
	for _, e := range ranked(eps) {
		out = append(out, strings.TrimPrefix(e.URL, "https://"))
	}
	return out
}

// normalizeAddr makes host:port of an address typed with --addr (a name, an IP, with or without the port).
func normalizeAddr(raw, listen string) (string, error) {
	raw = strings.TrimSpace(raw)
	host, port := raw, ""
	if h, p, err := net.SplitHostPort(raw); err == nil {
		host, port = h, p
	}
	host = strings.Trim(host, "[]")
	if port == "" {
		_, port, _ = net.SplitHostPort(listen)
	}
	if !ValidPort(port) {
		return "", newTextError("link-errors.address_port_required", nil, i18n.Args{"address": strconv.Quote(raw)})
	}
	if host == "" || strings.ContainsAny(host, "/?#@ ") {
		return "", newTextError("link-errors.address_invalid", nil, i18n.Args{"address": strconv.Quote(raw)})
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return "", newTextError("link-errors.address_zone", nil, i18n.Args{"address": strconv.Quote(raw)})
		}
		host = ip.String()
	}
	return net.JoinHostPort(host, port), nil
}

// ErrNoAddress: no address of this computer to put in a pairing link.
var ErrNoAddress = newTextError("link-errors.no_local_address", nil)

// PairInvite is what `persona-link pair --listen` hands the person: the link for the hub's admin page and what is in it.
type PairInvite struct {
	Link       string    `json:"link"`
	LegacyLink string    `json:"legacy_link,omitempty"` // same invitation for baseline hubs
	Addrs      []string  `json:"addrs"`
	FP         string    `json:"fp"`
	Listen     string    `json:"listen"`
	Expires    time.Time `json:"expires"`
}

// PairListen switches the link.json at path to passive mode, listening on listen (the one it had, else DefaultListen,
// when ""), with a new one-time pairing code that replaces any earlier one, and returns the link for the hub. addrs
// are the addresses to put in it (host or host:port); none: the ones found here (ListenCandidates). A hub paired
// before stays paired until another one uses the code.
func PairListen(path, listen string, addrs []string, now time.Time) (*PairInvite, error) {
	if listen != "" {
		l, err := NormalizeListen(listen)
		if err != nil {
			return nil, err
		}
		listen = l
	} else if c, err := Read(path); err == nil && c.Passive != nil {
		listen = c.Passive.ListenAddr()
	} else {
		listen = DefaultListen
	}
	var out []string
	for _, a := range addrs {
		n, err := normalizeAddr(a, listen)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		found, err := ListenCandidates(listen)
		if err != nil {
			return nil, newTextError("link-errors.local_addresses_failed", err)
		}
		if len(found) == 0 {
			return nil, ErrNoAddress
		}
		out = found
	}
	fp, err := Identity(TLSDir(path))
	if err != nil {
		return nil, newTextError("link-errors.tls_identity", err)
	}
	code := newCode()
	expires := now.Add(PairCodeLife)
	err = Update(path, func(c *Config) error {
		if c.Passive == nil {
			c.Passive = &Passive{}
		}
		c.Passive.Listen = listen
		c.Passive.Code = &PairCode{SHA256: hashHex(code), Expires: expires}
		c.Mode = ModePassive
		return nil
	})
	if err != nil {
		return nil, err
	}
	invite := ConnectorLink(out, fp, code)
	return &PairInvite{Link: invite, LegacyLink: LegacyPairLink(invite), Addrs: out, FP: fp, Listen: listen, Expires: expires}, nil
}

// ConnectorLink is the pairing link of passive mode: persona://connector?addr=…[&addr=…]&fp=…&code=…, addresses best first.
func ConnectorLink(addrs []string, fp, code string) string {
	var b strings.Builder
	b.WriteString("persona://connector?")
	for _, a := range addrs {
		b.WriteString("addr=" + url.QueryEscape(a) + "&")
	}
	b.WriteString("fp=" + url.QueryEscape(fp) + "&code=" + url.QueryEscape(code))
	return b.String()
}

// ParseConnectorLink reads a link ConnectorLink made: the addresses (host:port, best first), the pin and the code.
func ParseConnectorLink(raw string) (addrs []string, fp, code string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !pairScheme(u.Scheme) || u.Host != "connector" {
		return nil, "", "", newTextError("link-errors.connector_link_invalid", nil)
	}
	q := u.Query()
	fp, code = q.Get("fp"), q.Get("code")
	for _, a := range q["addr"] {
		if validDialAddr(a) {
			addrs = append(addrs, a)
		}
	}
	if fp == "" || code == "" || len(addrs) == 0 {
		return nil, "", "", newTextError("link-errors.connector_link_incomplete", nil)
	}
	return addrs, fp, code, nil
}

// SetMode switches the link.json at path to mode (ModeActive or ModePassive), keeping what either mode was paired
// with. listen, when not "", is where passive mode listens from now on. A running persona-link follows within a
// second or two.
func SetMode(path, mode, listen string) (*Config, error) {
	if mode != ModeActive && mode != ModePassive {
		return nil, newTextError("link-errors.mode_invalid", nil, i18n.Args{"active": ModeActive, "passive": ModePassive})
	}
	if listen != "" {
		l, err := NormalizeListen(listen)
		if err != nil {
			return nil, err
		}
		listen = l
	}
	var out Config
	err := Update(path, func(c *Config) error {
		c.Mode = mode
		if mode == ModePassive || listen != "" {
			if c.Passive == nil {
				c.Passive = &Passive{}
			}
			if listen != "" {
				c.Passive.Listen = listen
			} else if c.Passive.Listen == "" {
				c.Passive.Listen = DefaultListen
			}
		}
		out = *c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
