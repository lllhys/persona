package extension

// Hosts the owner allows a sandboxed extension in the admin UI (#5697, docs/extensions/extensions.md "Egress"): one
// specific host, with a port or on any port, beside what its manifest's permissions.network lists.

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/extension/egress"
	"github.com/nwverse/persona/server/internal/i18n"
)

// OwnerHost is one host the owner allowed: Port "" is any port.
type OwnerHost struct {
	Host string `json:"host"`
	Port string `json:"port,omitempty"`
}

// String is the host as written back: host, host:port, [v6]:port.
func (o OwnerHost) String() string {
	if o.Port == "" {
		if strings.Contains(o.Host, ":") {
			return "[" + o.Host + "]"
		}
		return o.Host
	}
	return net.JoinHostPort(o.Host, o.Port)
}

// Target is the egress proxy's rule for it: https or http, its port or any.
func (o OwnerHost) Target() egress.Target {
	return egress.Target{Host: o.Host, Port: o.Port, HTTP: true}
}

// ParseOwnerHost reads what the owner typed: a host name or an IP address, optionally with a port, or an http(s) URL
// whose host (and port) is taken. No wildcard, path, account or other scheme. Never this machine by name or a loopback
// address (the hint is host.persona.internal, which the extension's manifest asks for), and never a link-local,
// multicast, unspecified or metadata address; a name is checked again on every connection, whatever it resolves to.
func ParseOwnerHost(raw string) (OwnerHost, error) {
	s := strings.TrimSpace(raw)
	switch {
	case s == "":
		return OwnerHost{}, i18n.Err("extension-sandbox.allow_empty")
	case strings.Contains(s, "*"):
		return OwnerHost{}, i18n.Err("extension-sandbox.allow_wildcard")
	case strings.Contains(s, "://"):
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host == "" {
			return OwnerHost{}, i18n.Err("extension-sandbox.allow_form")
		}
		s = u.Host
	case strings.ContainsAny(s, "/@?# \t"):
		return OwnerHost{}, i18n.Err("extension-sandbox.allow_form")
	}
	host, port := s, ""
	if h, p, err := net.SplitHostPort(s); err == nil {
		host, port = h, p
	} else if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		host = s[1 : len(s)-1]
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return OwnerHost{}, i18n.Err("extension-sandbox.allow_port")
		}
	}
	if egress.IsLoopbackHost(host) || host == egress.HubHost || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return OwnerHost{}, i18n.Err("extension-sandbox.allow_loopback", i18n.Args{"name": egress.LocalHost})
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return OwnerHost{}, i18n.Err("extension-sandbox.allow_loopback", i18n.Args{"name": egress.LocalHost})
		}
		if egress.Unreachable(ip) {
			return OwnerHost{}, i18n.Err("extension-sandbox.allow_unreachable")
		}
		return OwnerHost{Host: ip.String(), Port: port}, nil
	}
	if host == "" || len(host) > 253 || strings.HasPrefix(host, "*.") || !hostPattern.MatchString(host) {
		return OwnerHost{}, i18n.Err("extension-sandbox.allow_form")
	}
	return OwnerHost{Host: host, Port: port}, nil
}
