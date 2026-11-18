// Package remoteimage fetches the pictures a reply names by address (Markdown `![alt](https://…)`) for the apps, so
// they only ever talk to their own hub (#9473, docs/protocol/protocol.md "Remote images"). The third party sees the
// hub's address, never a device's; the apps get the size they draw instead of the original.
//
// What it fetches: an http or https address on the open internet. The host is resolved once and every answer must be
// a public unicast address (Public); only those numbers are dialled (pinneddial), on every redirect too, so a second
// DNS answer cannot move a fetch onto this machine, the LAN or a cloud's metadata service. The request carries no
// cookies, no credentials and no Referer, except one retry with the image's own origin when its host refuses a request
// without one (hotlink protection; it names nothing the host does not already know).
//
// What it answers: JPEG, PNG, GIF (its first frame) and WebP are decoded, turned upright (EXIF), scaled to fit the size
// asked (never enlarged) and encoded again (JPEG, or PNG where transparent), which also drops their metadata. HEIC and
// AVIF, which Go cannot decode, pass as they are under a smaller byte limit. Anything else, SVG included (it can carry
// scripts), is refused: the type is read from the bytes, never from Content-Type.
//
// Limits: bytes and pixels per source, a time per fetch, fetches and decodes at once, fetches per caller (cache hits
// are free), a short memory of failed addresses, and a disk cache with a total cap that drops the least recently used.
package remoteimage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Sizes are the long sides (px) the apps may ask for; Snap rounds a request up to one of them.
var Sizes = []int{256, 512, 768, 1024, 1536, 2048}

// DefaultSize is the size served when none is asked for.
const DefaultSize = 1024

// The limits (docs/protocol/protocol.md "Remote images").
const (
	maxURL         = 4096
	maxSource      = 20 << 20 // bytes of a source the hub decodes
	maxPassThrough = 8 << 20  // bytes of a HEIC or AVIF passed as it is
	maxPixels      = 40_000_000
	maxRedirects   = 3
	fetchTimeout   = 15 * time.Second
	dialTimeout    = 5 * time.Second
	fetchSlots     = 6 // fetches at once, for every caller
	decodeSlots    = 2 // decodes at once: each may hold a large picture in memory
	cacheMax       = 512 << 20
	failedFor      = 2 * time.Minute // a failed address answers its error this long without being fetched again
	failedMost     = 1024
	// perCaller is how many fetches (cache misses) one caller may start in a minute, all of them at once at most.
	perCaller = 60
	// processing changes the cache keys when what is made from a source changes.
	processing = "v1"
)

// Codes of the refusals and failures (Error.Code), the route's JSON error codes.
const (
	CodeBadURL      = "bad_url"
	CodeNotAllowed  = "address_not_allowed"
	CodeTooLarge    = "too_large"
	CodeUnsupported = "unsupported_type"
	CodeBadImage    = "bad_image"
	CodeFetchFailed = "fetch_failed"
	CodeTimeout     = "timeout"
	CodeRateLimited = "rate_limited"
)

// Error is why an image could not be served: Code (above) and the HTTP status the route answers with.
type Error struct {
	Code   string
	Status int
	Detail string
	// RetryAfter is set with CodeRateLimited: when the caller may fetch again.
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return "remote image: " + e.Code
	}
	return "remote image: " + e.Code + ": " + e.Detail
}

func fail(code string, status int, format string, args ...any) *Error {
	return &Error{Code: code, Status: status, Detail: fmt.Sprintf(format, args...)}
}

// Image is what the apps get: the bytes, their type, and their size when the hub drew them (0 for a passed HEIC/AVIF).
type Image struct {
	Type          string
	Data          []byte
	Width, Height int
}

// Options set up a Fetcher.
type Options struct {
	// CacheDir is where the pictures are kept (created 0700); "" keeps none on disk.
	CacheDir string
	// CacheMax is the disk cache's cap in bytes (0: 512 MiB).
	CacheMax int64
	// DoH is the DNS-over-HTTPS JSON API asked for a host's real addresses when the resolver answers only a fake-IP
	// proxy's (doh.go), read when one is needed; nil or "" asks none, and such answers are refused.
	DoH  func() string
	Logf func(format string, args ...any)

	// Seams for tests.
	resolve func(ctx context.Context, host string) ([]net.IPAddr, error)
	dial    func(ctx context.Context, network, address string) (net.Conn, error)
	proxy   func(*http.Request) (*url.URL, error)
	now     func() time.Time
	timeout time.Duration // fetchTimeout
	// tlsInsecure lets tests use an httptest TLS server (its own certificate).
	tlsInsecure bool
}

// Fetcher serves remote images. One per manager; safe for concurrent use.
type Fetcher struct {
	o         Options
	transport *http.Transport
	fetches   chan struct{}
	decodes   chan struct{}
	cache     *diskCache
	limits    *limiter
	dohClient *http.Client

	mu      sync.Mutex
	doh     map[string]dohEntry
	calls   map[string]*call
	failed  map[string]failure
	sources map[string]*call
}

type call struct {
	done chan struct{}
	img  *Image
	src  []byte
	err  error
}

type failure struct {
	err   *Error
	until time.Time
}

// New makes a Fetcher.
func New(o Options) *Fetcher {
	if o.CacheMax <= 0 {
		o.CacheMax = cacheMax
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	if o.now == nil {
		o.now = time.Now
	}
	if o.timeout <= 0 {
		o.timeout = fetchTimeout
	}
	if o.resolve == nil {
		o.resolve = net.DefaultResolver.LookupIPAddr
	}
	if o.dial == nil {
		o.dial = (&net.Dialer{Timeout: dialTimeout}).DialContext
	}
	if o.proxy == nil {
		o.proxy = http.ProxyFromEnvironment
	}
	f := &Fetcher{
		o:       o,
		fetches: make(chan struct{}, fetchSlots),
		decodes: make(chan struct{}, decodeSlots),
		limits:  newLimiter(perCaller, time.Minute, o.now),
		calls:   map[string]*call{},
		failed:  map[string]failure{},
		sources: map[string]*call{},
		doh:     map[string]dohEntry{},
		// The DoH service is the operator's: reached as the machine reaches the internet (its proxy too).
		dohClient: &http.Client{Timeout: dohTimeout},
	}
	f.transport = f.newTransport()
	if o.CacheDir != "" {
		f.cache = &diskCache{dir: o.CacheDir, max: o.CacheMax, logf: o.Logf}
	}
	return f
}

// Snap is the served size for a requested one: the smallest of Sizes at least as large (the largest beyond them),
// DefaultSize for 0 or less.
func Snap(size int) int {
	if size <= 0 {
		return DefaultSize
	}
	for _, s := range Sizes {
		if size <= s {
			return s
		}
	}
	return Sizes[len(Sizes)-1]
}

func key(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Get is the image at rawURL scaled to fit size × size, for caller (who is asking, for the per-caller limit).
func (f *Fetcher) Get(ctx context.Context, caller, rawURL string, size int) (*Image, error) {
	u, err := CheckURL(rawURL)
	if err != nil {
		return nil, err
	}
	addr := u.String()
	size = Snap(size)
	out := key(processing, "out", strconv.Itoa(size), addr)
	if img := f.cache.image(out); img != nil {
		return img, nil
	}
	if e := f.failedLately(addr); e != nil {
		return nil, e
	}
	f.mu.Lock()
	c, joined := f.calls[out]
	if !joined {
		c = &call{done: make(chan struct{})}
		f.calls[out] = c
	}
	f.mu.Unlock()
	if !joined {
		// Not tied to the first asker's request: those who joined it still wait for the answer.
		go func() {
			c.img, c.err = f.make(context.WithoutCancel(ctx), caller, u, size, out)
			f.mu.Lock()
			delete(f.calls, out)
			f.mu.Unlock()
			close(c.done)
		}()
	}
	select {
	case <-c.done:
		return c.img, c.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// make fetches (or finds) the source, draws the size and keeps it.
func (f *Fetcher) make(ctx context.Context, caller string, u *url.URL, size int, out string) (*Image, error) {
	src, err := f.source(ctx, caller, u)
	if err != nil {
		return nil, err
	}
	select {
	case f.decodes <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	img, err := render(src, size)
	<-f.decodes
	if err != nil {
		f.remember(u.String(), err)
		return nil, err
	}
	f.cache.put(out, img)
	return img, nil
}

// source is the bytes at u: from the disk cache, or fetched once however many sizes wait for them.
func (f *Fetcher) source(ctx context.Context, caller string, u *url.URL) ([]byte, error) {
	addr := u.String()
	k := key(processing, "src", addr)
	if img := f.cache.image(k); img != nil {
		return img.Data, nil
	}
	f.mu.Lock()
	c, joined := f.sources[k]
	if !joined {
		c = &call{done: make(chan struct{})}
		f.sources[k] = c
	}
	f.mu.Unlock()
	if !joined {
		c.src, c.err = f.fetchLimited(ctx, caller, u)
		if c.err == nil {
			f.cache.put(k, &Image{Type: "application/octet-stream", Data: c.src})
		} else {
			f.remember(addr, c.err)
		}
		f.mu.Lock()
		delete(f.sources, k)
		f.mu.Unlock()
		close(c.done)
	}
	<-c.done
	return c.src, c.err
}

// fetchLimited is fetch within the caller's limit and the fetch slots.
func (f *Fetcher) fetchLimited(ctx context.Context, caller string, u *url.URL) ([]byte, error) {
	if wait, ok := f.limits.take(caller); !ok {
		return nil, &Error{Code: CodeRateLimited, Status: http.StatusTooManyRequests, Detail: "too many images fetched", RetryAfter: wait}
	}
	select {
	case f.fetches <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-f.fetches }()
	return f.fetch(ctx, u)
}

// failedLately is the error an address failed with in the last failedFor, if any.
func (f *Fetcher) failedLately(addr string) *Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if x, ok := f.failed[addr]; ok {
		if f.o.now().Before(x.until) {
			return x.err
		}
		delete(f.failed, addr)
	}
	return nil
}

// remember keeps a failure for failedFor, except a caller's limit (another caller may fetch it) or a cancel.
func (f *Fetcher) remember(addr string, err error) {
	var e *Error
	if !errors.As(err, &e) || e.Code == CodeRateLimited {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.failed) >= failedMost {
		clear(f.failed)
	}
	f.failed[addr] = failure{err: e, until: f.o.now().Add(failedFor)}
}
