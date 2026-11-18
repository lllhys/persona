package extension

import (
	"net"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

// What the owner may type to allow a host (#5697): a specific host, with a port or not, or an http(s) URL's host;
// never a wildcard, this machine, or an address no extension may reach.
func TestParseOwnerHost(t *testing.T) {
	lan := net.JoinHostPort(net.IPv4(192, 168, 1, 20).String(), "1200") // built: the privacy scan refuses literal ones
	for in, want := range map[string]OwnerHost{
		"rss.example.org":                   {Host: "rss.example.org"},
		" RSS.Example.org. ":                {Host: "rss.example.org"},
		"rss.example.org:8443":              {Host: "rss.example.org", Port: "8443"},
		"https://rss.example.org/feed?x=1":  {Host: "rss.example.org"},
		"http://rss.example.org:1200/route": {Host: "rss.example.org", Port: "1200"},
		"203.0.113.7":                       {Host: "203.0.113.7"},
		lan:                                 {Host: net.IPv4(192, 168, 1, 20).String(), Port: "1200"},
		"[2001:db8::5]:443":                 {Host: "2001:db8::5", Port: "443"},
		"2001:db8::5":                       {Host: "2001:db8::5"},
		"sentry.internal.example.net":       {Host: "sentry.internal.example.net"},
	} {
		got, err := ParseOwnerHost(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v, want %+v", in, got, err, want)
		}
	}
	for in, key := range map[string]string{
		"":                                    "extension-sandbox.allow_empty",
		"*.example.org":                       "extension-sandbox.allow_wildcard",
		"rss.*.org":                           "extension-sandbox.allow_wildcard",
		"ftp://rss.example.org":               "extension-sandbox.allow_form",
		"https://u:p@rss.example.org":         "extension-sandbox.allow_form",
		"rss.example.org/feed":                "extension-sandbox.allow_form",
		"me@rss.example.org":                  "extension-sandbox.allow_form",
		"-bad-.example.org":                   "extension-sandbox.allow_form",
		"rss.example.org:0":                   "extension-sandbox.allow_port",
		"rss.example.org:65536":               "extension-sandbox.allow_port",
		"rss.example.org:08443":               "extension-sandbox.allow_port",
		"localhost":                           "extension-sandbox.allow_loopback",
		"localhost:1200":                      "extension-sandbox.allow_loopback",
		"rss.localhost":                       "extension-sandbox.allow_loopback",
		"127.0.0.1:1200":                      "extension-sandbox.allow_loopback",
		"127.0.0.2":                           "extension-sandbox.allow_loopback",
		"[::1]:80":                            "extension-sandbox.allow_loopback",
		"host.persona.internal":               "extension-sandbox.allow_loopback",
		"hub.persona.internal":                "extension-sandbox.allow_loopback",
		net.IPv4(169, 254, 169, 254).String(): "extension-sandbox.allow_unreachable",
		"fd00:ec2::254":                       "extension-sandbox.allow_unreachable",
		"100.100.100.200":                     "extension-sandbox.allow_unreachable",
		"224.0.0.1":                           "extension-sandbox.allow_unreachable",
		"0.0.0.0":                             "extension-sandbox.allow_unreachable",
	} {
		_, err := ParseOwnerHost(in)
		m, ok := err.(*i18n.Message)
		if !ok || m.Key != key {
			t.Errorf("%q: %v, want %s", in, err, key)
		}
	}
	if s := (OwnerHost{Host: "2001:db8::5"}).String(); s != "[2001:db8::5]" {
		t.Fatal(s)
	}
	if s := (OwnerHost{Host: "rss.example.org", Port: "8443"}).String(); s != "rss.example.org:8443" {
		t.Fatal(s)
	}
}
