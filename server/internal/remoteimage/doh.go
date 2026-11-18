package remoteimage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"time"
)

// Behind a TUN proxy in fake-IP mode (Clash, mihomo, sing-box) the hub's resolver answers every outside name with an
// address of the proxy's own (#9550): 198.18/15 (sing-box's default; mihomo's 198.18/16 is in it) or sing-box's
// fc00::/18. Those are never a server's, so the check refuses them, and the real destination is the proxy's to decide.
// When every answer is such an address, the real ones are asked of a DNS-over-HTTPS service instead (port 443, which
// the proxy's DNS does not take over): they go through the same check and are dialled by number (through the proxy's
// tunnel). A failed lookup refuses the picture; a fake address is never let through. The service learns the picture's
// host name (docs/protocol/protocol.md "Remote images (#9473)").

// DefaultDoH is the DNS-over-HTTPS JSON API asked when manager.json names none.
const DefaultDoH = "https://dns.alidns.com/resolve"

const (
	dohTimeout = 5 * time.Second
	dohMost    = 64 << 10
	dohFor     = time.Minute // a host's real addresses are kept this long
)

var fakeNets = []netip.Prefix{netip.PrefixFrom(netip.AddrFrom4([4]byte{198, 18, 0, 0}), 15), netip.MustParsePrefix("fc00::/18")}

// fakeOnly says whether every one of ips is a fake-IP proxy's.
func fakeOnly(ips []net.IPAddr) bool {
	for _, a := range ips {
		ip, ok := netip.AddrFromSlice(a.IP)
		if !ok || !slices.ContainsFunc(fakeNets, func(p netip.Prefix) bool { return p.Contains(ip.Unmap()) }) {
			return false
		}
	}
	return len(ips) > 0
}

type dohEntry struct {
	ips   []net.IPAddr
	until time.Time
}

// realAddrs is host's addresses from the DoH service (A and AAAA), kept for dohFor.
func (f *Fetcher) realAddrs(ctx context.Context, host string) ([]net.IPAddr, error) {
	service := ""
	if f.o.DoH != nil {
		service = f.o.DoH()
	}
	if service == "" {
		return nil, errors.New("no DNS-over-HTTPS service")
	}
	f.mu.Lock()
	if e, ok := f.doh[host]; ok && f.o.now().Before(e.until) {
		f.mu.Unlock()
		return e.ips, nil
	}
	f.mu.Unlock()
	var ips []net.IPAddr
	var last error
	for _, kind := range []int{1, 28} { // A, AAAA
		got, err := f.dohQuery(ctx, service, host, kind)
		if err != nil {
			last = err
			continue
		}
		ips = append(ips, got...)
	}
	if len(ips) == 0 {
		if last == nil {
			last = errors.New("no address")
		}
		return nil, last
	}
	f.mu.Lock()
	if len(f.doh) > 256 {
		clear(f.doh)
	}
	f.doh[host] = dohEntry{ips: ips, until: f.o.now().Add(dohFor)}
	f.mu.Unlock()
	return ips, nil
}

// dohQuery asks the JSON API (Google's, Cloudflare's and Alibaba's shape) for one record type.
func (f *Fetcher) dohQuery(ctx context.Context, service, host string, kind int) ([]net.IPAddr, error) {
	u, err := url.Parse(service)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("DNS-over-HTTPS service %q is not an http(s) address", service)
	}
	q := u.Query()
	q.Set("name", host)
	q.Set("type", fmt.Sprint(kind))
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(ctx, dohTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/dns-json")
	res, err := f.dohClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DNS-over-HTTPS answered %d", res.StatusCode)
	}
	var answer struct {
		Status int
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		}
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, dohMost)).Decode(&answer); err != nil {
		return nil, fmt.Errorf("DNS-over-HTTPS answer does not read: %w", err)
	}
	if answer.Status != 0 {
		return nil, fmt.Errorf("DNS-over-HTTPS status %d", answer.Status)
	}
	var ips []net.IPAddr
	for _, a := range answer.Answer {
		if a.Type != kind {
			continue // the CNAMEs on the way
		}
		if ip := net.ParseIP(a.Data); ip != nil {
			ips = append(ips, net.IPAddr{IP: ip})
		}
	}
	return ips, nil
}
