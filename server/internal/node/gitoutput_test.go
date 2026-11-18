package node

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The tail of long git output starts on a whole character: a message in Chinese takes three bytes a character, and a
// cut at 4096 bytes from the end fell inside one.
func TestClipGitOutputStartsOnWholeCharacter(t *testing.T) {
	line := "无法连接服务"
	for _, extra := range []string{"", "a", "ab"} { // moves the cut to each byte of a character
		long := extra + strings.Repeat(line, 1000)
		got := clipGitOutput(long)
		tail, ok := strings.CutPrefix(got, "…")
		if !ok || !utf8.ValidString(got) {
			t.Fatalf("extra %q: valid UTF-8 %v, starts with an ellipsis %v: %.20q", extra, utf8.ValidString(got), ok, got)
		}
		if len(tail) > 4096 || len(tail) < 4096-3 || !strings.HasSuffix(long, tail) {
			t.Fatalf("extra %q: kept %d bytes, want the last 4093 to 4096 bytes of the output", extra, len(tail))
		}
	}
	if got := strings.Repeat(line, 200); clipGitOutput(got) != got {
		t.Fatal("output of at most 4096 bytes is kept as it is")
	}
	ascii := strings.Repeat("x", 5000)
	if got := clipGitOutput(ascii); got != "…"+ascii[5000-4096:] {
		t.Fatalf("ASCII output kept %d bytes", len(got))
	}
}
