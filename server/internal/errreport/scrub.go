// Package errreport is the generic error and crash reporting of Persona (docs/extensions/extensions.md "Error reports"): the
// reports of the hub, the manager and the apps are trimmed, scrubbed, fingerprinted, deduplicated and rate limited
// here before an extension with an error_reports contribution is given them. Nothing here knows where they go.
package errreport

import (
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/lazyre"
)

// Limits of one report (longer is cut, more is dropped), as the schema says (ErrorReport).
const (
	MessageMost    = 1024
	ValueMost      = 1024
	NameMost       = 256
	ExceptionsMost = 5
	FramesMost     = 64
	CrumbsMost     = 30
	CrumbMost      = 200
	TagsMost       = 16
	TagValueMost   = 64
	FieldMost      = 64
)

// Redacted stands in for what scrubbing took out.
const Redacted = "<redacted>"

// Scrubber takes out of a report what must never leave: user content, secrets, user names in paths, machine names,
// addresses. Names are the machine names and host names to hide (the hub's own, the user's computers).
type Scrubber struct {
	names []string
}

// NewScrubber hides these machine names and host names (case-insensitive; short ones are left: "a" is no name).
func NewScrubber(names ...string) *Scrubber {
	s := &Scrubber{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if utf8.RuneCountInString(n) < 3 || slices.Contains(s.names, strings.ToLower(n)) {
			continue
		}
		s.names = append(s.names, strings.ToLower(n))
		// a host name's first label is the machine's name too ("box" of "box.example.com")
		if i := strings.IndexByte(n, '.'); i >= 3 && !slices.Contains(s.names, strings.ToLower(n[:i])) {
			s.names = append(s.names, strings.ToLower(n[:i]))
		}
	}
	// longest first: "box.example.com" goes before "box"
	slices.SortFunc(s.names, func(a, b string) int { return len(b) - len(a) })
	return s
}

var (
	// secrets with their name: token=x, "password": "x", api_key: x. The names that only name a secret also count
	// after a prefix of words ending in _ or - (db_password, aws_secret_access_key, X-Auth-Token, GITHUB_TOKEN), and
	// the password words after plain letters too (PGPASSWORD); the name has to end in the secret's name (max_tokens,
	// password_file are not secrets). Short or common names (key, auth, sig, sid, cookie) count only on their own:
	// sort_key and monkey are not secrets, and neither is OLDPWD, whose pwd takes only a prefix ending in _ or -.
	namedSecret = lazyre.New(`(?i)\b(` +
		`(?:[a-z0-9]+[_-])*(?:[a-z0-9]*(?:password|passwd|passphrase)|pwd|secret|client_secret|secret[_-]?key|` +
		`secret[_-]?access[_-]?key|token|access_token|refresh_token|id_token|auth[_-]?token|api[_-]?key|apikey|` +
		`private[_-]?key|access[_-]?key(?:[_-]?id)?|credentials?|dsn|session_?id)` +
		`|authorization|proxy-authorization|cookie|set-cookie|sid|auth|key|signature|sig)` +
		`(["']?\s*[:=]\s*["']?)([^\s"'&,;]+)`)
	// an auth scheme and its credential: Bearer x, Basic x
	schemeSecret = lazyre.New(`(?i)\b(bearer|basic|token)(\s+)([A-Za-z0-9._~+/\-]{8,}=*)`)
	// tokens by their look: well-known prefixes, JWTs, long hex
	tokenLike = lazyre.New(`\b(pext_|sk-|sk_|ghp_|gho_|ghs_|ghu_|github_pat_|xox[abprs]-|glpat-|AKIA|ya29\.)[A-Za-z0-9._\-]{6,}|\beyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}(\.[A-Za-z0-9_\-]*)?|\b[0-9a-fA-F]{24,}\b`)
	// base64-ish blobs: long runs with both digits and letters (an identifier of only letters is code, not a key)
	blob  = lazyre.New(`[A-Za-z0-9+_\-]{32,}={0,2}`)
	email = lazyre.New(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// URLs keep scheme and path; the host, the user info, the query and the fragment go.
	urlRe = lazyre.New(`\b([a-zA-Z][a-zA-Z0-9+.\-]{1,15})://([^/\s"'<>]*)([^\s"'<>?#]*)(\?[^\s"'<>#]*)?(#[^\s"'<>]*)?`)
	ipv4  = lazyre.New(`\b(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}(:\d{1,5})?\b`)
	// IPv6: one or more groups, a ::, and any number of groups (a form ending in :: has no word boundary to end on,
	// hence its own alternative); or at least five groups (so 12:30:45 times are not addresses). The :: forms go
	// first: the five-group form also matches the groups before a ::, and would leave the rest behind.
	ipv6 = lazyre.New(`(?i)\[?\b([0-9a-f]{1,4}:){1,7}(:[0-9a-f]{1,4}){1,6}\b\]?|\[?\b([0-9a-f]{1,4}:){1,7}:\]?|\[?\b([0-9a-f]{1,4}:){4,7}[0-9a-f]{1,4}\b\]?|\[?::ffff:[0-9.]+\]?|\[::1\]`)
	// the user's name in home directories: /Users/x/, /home/x/, /data00/home/x/, C:\Users\x\
	homeDir = lazyre.New(`(?i)(/Users/|/home/|/data\d*/home/|/var/home/|/root\b|[A-Z]:\\Users\\|[A-Z]:/Users/)([^/\\\s"'<>]*)`)
	// quoted spans: what errors carry of the user's content most often. A quoted body may span lines (a message the
	// user wrote); only the single-quote form stays on one line, so apostrophes in prose do not pair across lines.
	quoted = lazyre.New("\"[^\"]*\"|“[^”]*”|「[^」]*」|『[^』]*』|`[^`]*`|'[^'\\n]{2,}'")
)

// Text scrubs one string of a report.
func (s *Scrubber) Text(v string) string {
	if v == "" {
		return v
	}
	v = strings.ToValidUTF8(v, "")
	v = urlRe.ReplaceAllStringFunc(v, func(m string) string {
		p := urlRe.FindStringSubmatch(m)
		return p[1] + "://<host>" + p[3]
	})
	v = schemeSecret.ReplaceAllString(v, "${1}${2}"+Redacted)
	v = namedSecret.ReplaceAllString(v, "${1}${2}"+Redacted)
	v = tokenLike.ReplaceAllString(v, Redacted)
	v = blob.ReplaceAllStringFunc(v, func(m string) string {
		if strings.ContainsAny(m, "0123456789") && strings.IndexFunc(m, unicode.IsLetter) >= 0 {
			return Redacted
		}
		return m
	})
	v = email.ReplaceAllString(v, "<email>")
	v = homeDir.ReplaceAllString(v, "~")
	v = ipv6.ReplaceAllString(v, "<ip>")
	v = ipv4.ReplaceAllString(v, "<ip>")
	for _, n := range s.names {
		v = replaceFold(v, n, "<machine>")
	}
	return v
}

// Message scrubs a message or an exception's value: Text, and quoted spans become "<redacted>" (an error that
// quotes what the user wrote must not carry it).
func (s *Scrubber) Message(v string) string {
	v = quoted.ReplaceAllString(v, `"`+Redacted+`"`)
	return s.Text(v)
}

// replaceFold replaces every case-insensitive match of old (lowercase, as strings.ToLower gives it) in s. The match
// runs on the lowercase of s. A rune whose lowercase has another byte length (U+0130, the Kelvin sign) shifts the
// lowercase against s, so outside ASCII every lowercase byte keeps where its rune starts in s.
func replaceFold(s, old, repl string) string {
	if old == "" {
		return s
	}
	lower := strings.ToLower(s)
	if !strings.Contains(lower, old) {
		return s
	}
	var from []int // from[j] is where the rune of lower[j] starts in s; nil for ASCII, where both line up byte for byte
	if !isASCII(s) {
		lower, from = foldMap(s)
	}
	at := func(j int) int { // where lower[j:] starts in s
		if from == nil {
			return j
		}
		return from[j]
	}
	var b strings.Builder
	done, next := 0, 0 // done: s is written up to here; next: lower is searched from here
	for {
		i := strings.Index(lower[next:], old)
		if i < 0 {
			b.WriteString(s[done:])
			return b.String()
		}
		i += next
		b.WriteString(s[done:at(i)])
		b.WriteString(repl)
		next = i + len(old)
		done = at(next)
	}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// foldMap lowercases s rune by rune as strings.ToLower does, and keeps from[j], the offset in s of the rune that gave
// lower[j] (from[len(lower)] is len(s)).
func foldMap(s string) (lower string, from []int) {
	var b strings.Builder
	b.Grow(len(s))
	from = make([]int, 0, len(s)+1)
	for i, r := range s { // an invalid byte comes as utf8.RuneError and is written as U+FFFD, as strings.ToLower does
		n, _ := b.WriteRune(unicode.ToLower(r))
		for range n {
			from = append(from, i)
		}
	}
	return b.String(), append(from, len(s))
}

// Report scrubs and trims r in place: every string a person or a machine could be told by; ids, enums and numbers
// are checked by Normalize.
func (s *Scrubber) Report(r *wire.ErrorReport) {
	r.Message = cut(s.Message(r.Message), MessageMost)
	r.Release = cut(s.Text(r.Release), FieldMost)
	r.Build = cut(s.Text(r.Build), FieldMost)
	r.Commit = cut(s.Text(r.Commit), FieldMost)
	r.OS = cut(s.Text(r.OS), 32)
	r.OSVersion = cut(s.Text(r.OSVersion), FieldMost)
	r.DeviceModel = cut(s.Text(r.DeviceModel), FieldMost)
	if len(r.Exceptions) > ExceptionsMost {
		r.Exceptions = r.Exceptions[len(r.Exceptions)-ExceptionsMost:] // the innermost cause is last: keep it
	}
	for i := range r.Exceptions {
		e := &r.Exceptions[i]
		e.Type = cut(s.Text(e.Type), NameMost)
		e.Value = cut(s.Message(e.Value), ValueMost)
		if len(e.Frames) > FramesMost {
			e.Frames = e.Frames[len(e.Frames)-FramesMost:] // innermost last: the frames nearest the error stay
		}
		for j := range e.Frames {
			f := &e.Frames[j]
			f.Function = cut(s.Text(f.Function), NameMost)
			f.Module = cut(s.Text(f.Module), NameMost)
			f.File = cut(s.Text(f.File), NameMost)
			f.Line, f.Column = max(f.Line, 0), max(f.Column, 0)
		}
	}
	if len(r.Breadcrumbs) > CrumbsMost {
		r.Breadcrumbs = r.Breadcrumbs[len(r.Breadcrumbs)-CrumbsMost:]
	}
	for i := range r.Breadcrumbs {
		c := &r.Breadcrumbs[i]
		c.Category = cut(s.Text(c.Category), 64)
		c.Message = cut(s.Message(c.Message), CrumbMost)
		c.Level = crumbLevel(c.Level)
		c.Time = crumbTime(c.Time)
	}
	if len(r.Tags) > 0 {
		tags := map[string]string{}
		for _, k := range slices.Sorted(mapKeys(r.Tags)) {
			if len(tags) == TagsMost || !tagKey.MatchString(k) {
				continue
			}
			tags[k] = cut(s.Message(r.Tags[k]), TagValueMost)
		}
		r.Tags = tags
	}
}

// crumbLevels are the levels a breadcrumb may have; any other value is dropped, so the field never carries free text.
var crumbLevels = []string{"debug", "info", "warning", "error", "fatal"}

func crumbLevel(v string) string {
	if v = strings.ToLower(v); slices.Contains(crumbLevels, v) {
		return v
	}
	return ""
}

// crumbTime keeps a breadcrumb's time only as an RFC 3339 time, in UTC; anything else is dropped.
func crumbTime(v string) string {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

var tagKey = lazyre.New(`^[a-z][a-z0-9_.]{0,31}$`)

func mapKeys(m map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// cut keeps at most n bytes of v, on a rune boundary.
func cut(v string, n int) string {
	if len(v) <= n {
		return v
	}
	for n > 0 && !utf8.RuneStart(v[n]) {
		n--
	}
	return v[:n]
}
