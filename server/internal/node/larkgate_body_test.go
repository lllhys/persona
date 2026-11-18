package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// A body file of Chinese text is cut to whole characters: it takes three bytes a character, and a cut at 4000 bytes
// fell inside one, which the approval card then showed as a broken character before the ellipsis.
func TestReadBodyFileCutsOnWholeCharacters(t *testing.T) {
	dir := t.TempDir()
	write := func(text string) string {
		p := filepath.Join(dir, "body.json")
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	const unit = "无法连接服务"
	over := strings.Repeat(unit, 1000) // 6000 characters
	got := readBodyFile(write(over), dir)
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "…") || utf8.RuneCountInString(got) != 4001 {
		t.Fatalf("a body over the limit: valid UTF-8 %v, %d characters, ends %q", utf8.ValidString(got), utf8.RuneCountInString(got), got[max(0, len(got)-12):])
	}
	// Over 4000 bytes but under 4000 characters: kept whole.
	whole := strings.Repeat(unit, 400) // 2400 characters, 7200 bytes
	if got := readBodyFile(write(whole), dir); got != whole {
		t.Fatalf("a body of 2400 characters was cut to %d characters", utf8.RuneCountInString(got))
	}
	ascii := strings.Repeat("x", 5000)
	if got := readBodyFile(write(ascii), dir); got != ascii[:4000]+"…" {
		t.Fatalf("an ASCII body kept %d bytes", len(got))
	}
}
