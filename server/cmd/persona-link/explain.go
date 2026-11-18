package main

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/link"
)

// explain turns the two failures of a pairing a person can fix from here into one plain sentence that says how,
// with the raw error kept on the next line for debugging (#5045): the hub cannot be reached, or it refused the
// pairing link. Anything else is printed as it was.
func explain(err error) string {
	return explainLanguage(i18n.Terminal(), err)
}

func explainLanguage(l i18n.Lang, err error) string {
	raw := link.ErrorText(l, err)
	var se *link.SSHError
	switch {
	case rejected(err):
		return i18n.T(l, "link-cli.explain_rejected", i18n.Args{"raw": raw})
	case errors.As(err, &se):
		return se.Text(l)
	case unreachable(err):
		return i18n.T(l, "link-cli.explain_unreachable", i18n.Args{"hosts": terminalJoin(hostsOf(err)), "raw": raw})
	}
	return raw
}

// rejected: some candidate's hub answered the pairing with "no" to its token. link.Pair says so as text, "The hub rejected:"
// and the hub's answer: its 401 body ("invalid token") or, from a proxy with no JSON, the status ("401 Unauthorized").
// A 403 (a link for another machine) or a 429 is a different matter and stays as it is.
func rejected(err error) bool {
	for _, e := range leavesOf(err) {
		s := e.Error()
		if i := strings.LastIndex(s, "hub 拒绝了："); i >= 0 { // i18n-ignore: detects the canonical hub-refusal prefix before presentation
			answer := s[i+len("hub 拒绝了："):] // i18n-ignore: detects the canonical hub-refusal prefix before presentation
			if answer == "invalid token" || strings.HasPrefix(answer, "401 ") {
				return true
			}
		}
	}
	return false
}

// unreachable: every candidate's request to the hub failed with no answer from a hub (refused, unreachable, no such
// name, or timed out: a hub that is up but hung reads the same here). Only requests count (each failure carries its URL), so a local network error elsewhere is not
// taken for the hub's.
func unreachable(err error) bool {
	leaves := leavesOf(err)
	for _, e := range leaves {
		if !errors.As(e, new(*url.Error)) || !networkFailure(e) {
			return false
		}
	}
	return len(leaves) > 0
}

// networkFailure: no connection to talk over. A failed TCP dial is an *net.OpError with Op "dial" on every platform
// (Windows' refused connection is WSAECONNREFUSED, which syscall.ECONNREFUSED does not match); TLS failures are not.
func networkFailure(err error) bool {
	var oe *net.OpError
	var dns *net.DNSError
	var ne net.Error
	return errors.As(err, &oe) && oe.Op == "dial" || errors.As(err, &dns) ||
		errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout()
}

// leavesOf is err split at each errors.Join (one per candidate address in a pairing race).
func leavesOf(err error) []error {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if joined, ok := e.(interface{ Unwrap() []error }); ok {
			var out []error
			for _, part := range joined.Unwrap() {
				out = append(out, leavesOf(part)...)
			}
			return out
		}
	}
	return []error{err}
}

// hostsOf is the host:port of each request that failed, in order, once each.
func hostsOf(err error) []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range leavesOf(err) {
		var ue *url.Error
		if !errors.As(e, &ue) {
			continue
		}
		if u, perr := url.Parse(ue.URL); perr == nil && u.Host != "" && !seen[u.Host] {
			seen[u.Host] = true
			out = append(out, u.Host)
		}
	}
	return out
}

// nodeHint follows "The hub cannot connect to this computer's node (...)": how to check the resident node, unless the error already says
// (link.nodeMissing names the command that runs it).
func nodeHint(nodeErr string) string {
	if strings.Contains(nodeErr, link.NodeDownMarker) {
		return ""
	}
	return terminalText("link-cli.node_hint")
}
