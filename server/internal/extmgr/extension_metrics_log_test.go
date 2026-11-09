package extmgr

import (
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/nwverse/persona/server/internal/extension/host"
)

// redactSecretsByPatterns is redactSecrets with every pattern run on every line, as it was before the words were
// looked for: what redactSecrets must give back, byte for byte.
func redactSecretsByPatterns(line string) string {
	for _, p := range secretPatterns {
		line = p.re.ReplaceAllString(line, "${1}***")
	}
	return line
}

// redactCases are lines and what redaction makes of them (want "" when the line stays as it is).
var redactCases = []struct{ in, want string }{
	{"", ""},
	{"nothing to hide", ""},
	{"AuThOrIzAtIoN: abcdef123", "AuThOrIzAtIoN: ***"},
	{"Authorization: Bearer abcdefghijklmnop", "Authorization: ***"},
	{`"authorization": "Basic dXNlcjpwYXNz"`, `"authorization": "***"`},
	{"Cookie: session=abcdef123; theme=dark", "Cookie: ***; theme=dark"},
	{"COOKIE=abcdef", "COOKIE=***"},
	{"cookie=abcde", ""}, // 5: under {6,}
	{"sent with BeArEr abcdefgh", "sent with BeArEr ***"},
	{"sent with bearer abcdefg", ""}, // 7: under {8,}
	{"ToKeN=abcdef rest", "ToKeN=*** rest"},
	{"token=abcde rest", ""},
	{"SeCrEt: abcdef", "SeCrEt: ***"},
	{`{"password":"hunter2hunter2"}`, `{"password":"***"}`},
	{"PASSWD=hunter22", "PASSWD=***"},
	{`{"api_key": "sk-123456789"}`, `{"api_key": "***"}`},
	{"Api-Key: abcdef12", "Api-Key: ***"},
	{"ACCESSKEY=abcdef12", "ACCESSKEY=***"},
	{"access_key = 'abcdef12'", "access_key = '***'"},
	{"GET https://example.com/a?sig=abc&key=xyz&page=2", "GET https://example.com/a?sig=***&key=***&page=2"},
	{"GET /x?SIG=a", "GET /x?SIG=***"},
	{"GET /x?access_token=abc&y=1", "GET /x?access_token=***&y=1"},
	{"to\u212aen=abcdef123", "to\u212aen=***"},            // U+212A (Kelvin sign) folds into k
	{"\u017fecret=abcdef123", "\u017fecret=***"},          // U+017F (long s) folds into s
	{"GET /x?\u212aey=abc", "GET /x?\u212aey=***"},        // and in a URL
	{"bearer \u017f\u017f\u017f\u017fabcd", "bearer ***"}, // the class [A-Za-z] under (?i) holds U+017F too
	{"\u212a alone", ""},
	{"token=\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9 caf\u00e9", "token=*** caf\u00e9"}, // {6,} counts runes
	{"token=\u4e00\u4e8c\u4e09\u56db\u4e94", ""},                                    // five runes, fifteen bytes
	{"na\u00efve caf\u00e9 \u2014 r\u00e9sum\u00e9 \u2026 \u20ac 42 \u2122", ""},
	{"\u0130 \u0131 \u212b \u03a3\u03c3\u03c2 \u0416\u0436", ""}, // fold into no ASCII letter
	{"\xff\xfetoken=abcdef\xc5", "\xff\xfetoken=***"},
	{"monkey keyboard tokenizer design signal secretary", ""},
	{"Passwords: tokens=abcdef", ""},
	{"token: abcdef, Authorization: Bearer abcdefghij, ?sig=s1&key=k2", "token: ***, Authorization: ***, ?sig=***&key=***"},
}

func TestRedactSecretsIsEveryPatternsRedaction(t *testing.T) {
	for _, c := range redactCases {
		want := c.want
		if want == "" {
			want = c.in
		}
		if got := redactSecretsByPatterns(c.in); got != want {
			t.Errorf("patterns: %q → %q, want %q", c.in, got, want)
		}
		checkRedaction(t, c.in)
	}
}

// checkRedaction fails when redactSecrets gives line back otherwise than every pattern does, or when a pattern
// matches line without one of its words in it.
func checkRedaction(t *testing.T, line string) {
	t.Helper()
	if got, want := redactSecrets(line), redactSecretsByPatterns(line); got != want {
		t.Errorf("%q → %q, the patterns give %q", line, got, want)
	}
	words := secretWordsIn(line)
	for i, p := range secretPatterns {
		if p.re.MatchString(line) && words&p.words == 0 {
			t.Errorf("pattern %d matches %q, which has none of its words (%08b of %08b)", i, line, words, p.words)
		}
	}
}

func FuzzRedactSecrets(f *testing.F) {
	for _, c := range redactCases {
		f.Add(c.in)
	}
	f.Add(benchPlainShort)
	f.Add(benchWithSecret)
	f.Fuzz(checkRedaction)
}

// The words are looked for in ASCII, case aside, plus the two runes secretWordsIn knows: no other rune may fold into
// an ASCII letter, or (?i) would match a word written with it that secretWordsIn does not see.
func TestOnlyTwoRunesFoldIntoASCIILetters(t *testing.T) {
	for r := rune(0x80); r <= unicode.MaxRune; r++ {
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if f < 0x80 && unicode.IsLetter(f) && r != 'ſ' && r != 'K' {
				t.Errorf("%U folds into %q: secretWordsIn must look for it", r, f)
			}
		}
	}
}

var redactSink string

func TestRedactSecretsCopiesNoPlainLine(t *testing.T) {
	for _, line := range []string{benchPlainShort, benchPlainLong} {
		if n := testing.AllocsPerRun(100, func() { redactSink = redactSecrets(line) }); n != 0 {
			t.Errorf("%d-byte line without a secret: %v allocations, want 0", len(line), n)
		}
	}
}

// Redaction runs before the stats lock is taken: a line being redacted holds up neither the counts nor the readers.
func TestExtensionLogRedactsOutsideTheLock(t *testing.T) {
	m, _ := testMetrics(time.Now())
	entered, release := make(chan struct{}), make(chan struct{})
	m.redact = func(l string) string {
		if strings.Contains(l, "slow") {
			entered <- struct{}{}
			<-release
		}
		return l
	}
	s := m.of("demo")
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Log(host.LogProgram, "a slow line")
		s.Run(host.RunExit, "a slow exit")
	}()
	for _, what := range []string{"log line", "exit reason"} {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: redaction did not start", what)
		}
		counted := make(chan struct{})
		go func() {
			defer close(counted)
			s.Count("tick", 1)
			s.Lines(-1, "", 0)
		}()
		select {
		case <-counted:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: Count and Lines waited for the redaction", what)
		}
		release <- struct{}{}
	}
	<-done
	if lines, _ := s.Lines(-1, "", 0); len(lines) != 1 || lines[0].Text != "a slow line" {
		t.Fatalf("lines: %v", lines)
	}
}

// benchPlainShort is a 110-byte line without a secret, benchPlainLong a 3.5 KB one.
const benchPlainShort = "2026-11-09T10:02:03.456Z INFO connector synced 42 messages from inbox in 315ms, cursor moved to 18734 (page 4)"

var benchPlainLong = "[" + strings.Repeat(`{"id":1234,"name":"report-weekly","state":"done","items":[1,2,3],"path":"/var/lib/app/data/file.json"},`, 34) + "]"

const benchWithSecret = "GET /api/v1/feed?token=abcdef0123456789&page=2 sent with Authorization: Bearer abcdefghijklmnopqrstuvwxyz"

func BenchmarkRedactSecrets(b *testing.B) {
	for _, c := range []struct{ name, line string }{
		{"plain_short", benchPlainShort},
		{"plain_long", benchPlainLong},
		{"with_secret", benchWithSecret},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				redactSecrets(c.line)
			}
		})
	}
}

func BenchmarkStatsLog(b *testing.B) {
	m, _ := testMetrics(time.Now())
	s := m.of("demo")
	b.ReportAllocs()
	for b.Loop() {
		s.Log(host.LogProgram, benchPlainShort)
	}
}
