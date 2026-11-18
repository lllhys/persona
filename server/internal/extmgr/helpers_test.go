package extmgr

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// A tool result for the model loses avatars, thumbnails and empty values, and keeps the rest.
func TestCompactJSONPrunes(t *testing.T) {
	out := compactJSON(json.RawMessage(`{"a":{"avatarUrl":"x","name":"n","empty":"","no":false},"list":[{"thumbnail":"t","v":1}]}`), 1000)
	if strings.Contains(out, "avatar") || strings.Contains(out, "thumbnail") || strings.Contains(out, "empty") || !strings.Contains(out, `"name":"n"`) {
		t.Fatalf("got %s", out)
	}
}

// A long answer is cut on a whole character: a cut inside a Chinese character would leave invalid UTF-8 in the text the
// model reads.
func TestCompactJSONCutsOnWholeCharacter(t *testing.T) {
	raw, err := json.Marshal(map[string]string{"text": strings.Repeat("好", 100)})
	if err != nil {
		t.Fatal(err)
	}
	// {"text":" is 9 bytes, so a limit of 13 ends inside the second character.
	out := compactJSON(raw, 13)
	if !utf8.ValidString(out) {
		t.Fatalf("cut inside a character: %q", out)
	}
	if !strings.HasPrefix(out, `{"text":"好`) || !strings.Contains(out, "(cut;") {
		t.Fatalf("got %q", out)
	}
}
