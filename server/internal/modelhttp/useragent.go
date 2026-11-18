// Package modelhttp identifies Persona's own requests to model gateways.
package modelhttp

import (
	"net/http"
	"strings"
)

var userAgent = "Persona/dev"

// SetVersion is called at process startup with the version injected into the binary.
func SetVersion(v string) {
	if v != "" {
		userAgent = "Persona/" + v
	}
}

// UserAgent returns the identity sent to model gateways.
func UserAgent() string { return userAgent }

// SetDefaultUserAgent preserves a caller-supplied header, including an empty value.
func SetDefaultUserAgent(h http.Header) {
	// Forwarded headers may use non-canonical names. net/http recognizes only the
	// canonical key when deciding whether to add its own User-Agent.
	for name, values := range h {
		if name != "User-Agent" && strings.EqualFold(name, "User-Agent") {
			if _, ok := h["User-Agent"]; !ok {
				h["User-Agent"] = values
			}
			delete(h, name)
		}
	}
	if _, ok := h["User-Agent"]; !ok {
		h.Set("User-Agent", UserAgent())
	}
}
