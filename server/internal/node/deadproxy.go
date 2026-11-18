package node

import (
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A proxy that does not answer (a proxy app that has quit, still named in the shell or in ~/.claude/settings.json)
// holds every request of a launcher behind it until the time runs out: Settings › Models' test said only that no answer
// came, for every provider not in NO_PROXY (#5953). A managed launcher that sets no proxy of its own drops such a proxy
// when it starts, but only when its gateway answers without it: one reachable only through the proxy keeps it, and the
// proxy can still come back while the session runs (an app restarting, a laptop just awake). One that answers stays.

// proxyVars: the variables Claude Code (Node's fetch) takes a proxy from.
var proxyVars = []string{"HTTPS_PROXY", "HTTP_PROXY", "https_proxy", "http_proxy", "ALL_PROXY", "all_proxy"}

// proxyDialTimeout: how long a proxy may take to accept a connection before it counts as not answering; it is tried
// twice, proxyRetryAfter apart.
var proxyDialTimeout = time.Second

var proxyRetryAfter = 300 * time.Millisecond

var proxySeen struct {
	sync.Mutex
	m map[string]proxyCheck
}

type proxyCheck struct {
	ok bool
	at time.Time
}

// proxyAnswers: whether something listens where the proxy URL raw points (a TCP connection within proxyDialTimeout,
// the second try counting too). What cannot be read as an address is left alone: not ours to judge.
func proxyAnswers(raw string) bool {
	addr := proxyAddr(raw)
	return addr == "" || dialAnswers(addr, 2)
}

// dialAnswers: whether addr (host:port) takes a TCP connection in one of tries. Remembered for a minute per address.
func dialAnswers(addr string, tries int) bool {
	proxySeen.Lock()
	if c, ok := proxySeen.m[addr]; ok && time.Since(c.at) < time.Minute {
		proxySeen.Unlock()
		return c.ok
	}
	proxySeen.Unlock()
	ok := false
	for i := 0; i < tries && !ok; i++ {
		if i > 0 {
			time.Sleep(proxyRetryAfter)
		}
		if conn, err := net.DialTimeout("tcp", addr, proxyDialTimeout); err == nil {
			_ = conn.Close()
			ok = true
		}
	}
	proxySeen.Lock()
	if proxySeen.m == nil {
		proxySeen.m = map[string]proxyCheck{}
	}
	proxySeen.m[addr] = proxyCheck{ok: ok, at: time.Now()}
	proxySeen.Unlock()
	return ok
}

// gatewayAnswers: whether the gateway at base (a launcher's Base URL; none: Anthropic's API) takes a direct connection.
func gatewayAnswers(base string) bool {
	if strings.TrimSpace(base) == "" {
		base = "https://" + proto.AnthropicAPIHost
	}
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Hostname() == "" {
		return false
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80"}[strings.ToLower(u.Scheme)]
		if port == "" {
			port = "443"
		}
	}
	return dialAnswers(net.JoinHostPort(u.Hostname(), port), 1)
}

// proxyAddr is the host:port a proxy URL (or a bare host:port) names, "" when it names none.
func proxyAddr(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		if u, err = url.Parse("http://" + raw); err != nil || u.Host == "" {
			return ""
		}
	}
	if u.Port() != "" {
		return u.Host
	}
	port := map[string]string{"http": "80", "https": "443", "socks": "1080", "socks5": "1080", "socks5h": "1080"}[strings.ToLower(u.Scheme)]
	if port == "" {
		return ""
	}
	return net.JoinHostPort(u.Hostname(), port)
}

// proxyShown is a proxy as a message names it: its scheme, host and port, never a user or password in it.
func proxyShown(raw string) string {
	if u, err := url.Parse(strings.TrimSpace(raw)); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return proxyAddr(raw)
}

// withoutDeadProxies is env (KEY=value entries) without the proxy variables it inherited, the ones own (what the
// launcher sets) does not name, whose proxy does not answer, when the gateway at base answers without them. warn is
// told of each one dropped.
func withoutDeadProxies(env, own []string, base string, warn func(string)) []string {
	set := map[string]bool{}
	for _, kv := range own {
		k, _, _ := strings.Cut(kv, "=")
		set[k] = true
	}
	dead := func(kv string) bool {
		k, v, _ := strings.Cut(kv, "=")
		return !set[k] && slices.Contains(proxyVars, k) && v != "" && !proxyAnswers(v)
	}
	if !slices.ContainsFunc(env, dead) || !gatewayAnswers(base) {
		return env
	}
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		if !dead(kv) {
			return false
		}
		if warn != nil {
			k, v, _ := strings.Cut(kv, "=")
			warn("the proxy " + proxyShown(v) + " (" + k + ") does not answer; going without it")
		}
		return true
	})
}

// userSettingsEnv is the env of the user's settings.json, in the Claude config directory the launcher's claude uses
// (env's CLAUDE_CONFIG_DIR, else the legacy default): what Claude Code applies over the environment it starts with. nil when
// there is none.
func (n *Node) userSettingsEnv(env map[string]any, configDir ...string) map[string]any {
	dir := n.defaultClaudeDir()
	if len(configDir) > 0 {
		dir = configDir[0]
	}
	if d, ok := env["CLAUDE_CONFIG_DIR"].(string); ok && d != "" {
		dir = proto.ExpandHome(d, n.home)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return nil
	}
	var user struct {
		Env map[string]any `json:"env"`
	}
	if json.Unmarshal(b, &user) != nil {
		return nil
	}
	return user.Env
}

// userNoProxy is the bypass lists the user's settings env sets for l's claude, by spelling (userSettingsEnv): what
// Claude Code applies over the environment it starts with, so they come before the shell's or the node's (#6238).
func (n *Node) userNoProxy(l proto.ManagedLauncher, configDir ...string) map[string]string {
	env := map[string]any{}
	if len(configDir) == 0 {
		env["CLAUDE_CONFIG_DIR"] = providerEnv(l, "", "", false)["CLAUDE_CONFIG_DIR"]
	}
	user := n.userSettingsEnv(env, configDir...)
	out := map[string]string{}
	for _, k := range []string{"NO_PROXY", "no_proxy"} {
		if v, _ := user[k].(string); v != "" {
			out[k] = v
		}
	}
	return out
}

// deadSettingsProxies: the proxy variables the user's settings env (settings.json of the Claude config directory the
// launcher's claude uses, which Claude Code applies over the environment it starts with) names a proxy in that does
// not answer, for a launcher's settings env that sets none of them itself, when its gateway at base answers without:
// each to be set empty there (settings env cannot unset).
func (n *Node) deadSettingsProxies(env map[string]any, base string, configDir ...string) []string {
	user := n.userSettingsEnv(env, configDir...)
	var dead []string
	for _, k := range proxyVars {
		if _, ours := env[k]; ours {
			continue
		}
		if v, ok := user[k].(string); ok && v != "" && !proxyAnswers(v) {
			dead = append(dead, k)
		}
	}
	if len(dead) == 0 || !gatewayAnswers(base) {
		return nil
	}
	return dead
}
