package egress

import (
	"crypto/rand"
	"encoding/hex"
	"slices"
	"sync"
	"time"
)

// A settings check's own way out (#8459, docs/extensions/extensions.md "Settings the user fills in"). A program asked
// to check settings it has not been given yet must reach the address they name. Only requests sent with the check's
// own credential get there: the program's ordinary credential keeps the saved addresses, also while the check runs
// (#1180). The check's credential works until the check answers (revoke) or its time is up, whichever comes first;
// the connections opened with it are closed then.

// check is one settings check's credential: the extension it is for, the addresses its proposed settings name, when it
// stops working by itself, and the connections opened with it.
type check struct {
	ext     string
	cred    string
	targets []Target
	expires time.Time
	leases  map[*ioLease]struct{}
}

// Check gives ext's program a credential for one settings check. A request with it is held to ext's rule, with
// targets in place of the addresses its saved settings name (Rule.Targets). It works until revoke is called or ttl is
// up. ok is false when ext is not registered (its program is not behind the proxy).
func (p *Proxy) Check(ext string, targets []Target, ttl time.Duration) (cred string, revoke func(), ok bool) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	cred = hex.EncodeToString(b)
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.rules[ext]; !ok {
		return "", nil, false
	}
	for _, c := range p.checks {
		if !now.Before(c.expires) {
			p.endCheckLocked(c)
		}
	}
	if p.checks == nil {
		p.checks = map[string]*check{}
	}
	c := &check{ext: ext, cred: cred, targets: slices.Clone(targets), expires: now.Add(ttl), leases: map[*ioLease]struct{}{}}
	p.checks[cred] = c
	var once sync.Once
	return cred, func() {
		once.Do(func() {
			p.mu.Lock()
			p.endCheckLocked(c)
			p.mu.Unlock()
		})
	}, true
}

// endCheckLocked revokes c: its credential stops working, and the connections opened with it are closed.
func (p *Proxy) endCheckLocked(c *check) {
	if p.checks[c.cred] == c {
		delete(p.checks, c.cred)
	}
	for lease := range c.leases {
		lease.cancel()
	}
	clear(c.leases)
}

// endChecksLocked revokes every check of ext (its program restarted or stopped).
func (p *Proxy) endChecksLocked(ext string) {
	for _, c := range p.checks {
		if c.ext == ext {
			p.endCheckLocked(c)
		}
	}
}

// liveCheckLocked is the check of credential cred, while it works.
func (p *Proxy) liveCheckLocked(cred string) *check {
	c := p.checks[cred]
	if c == nil || !time.Now().Before(c.expires) {
		return nil
	}
	return c
}
