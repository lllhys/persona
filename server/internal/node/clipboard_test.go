package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

// The clipboard goes through pbcopy and pbpaste (fakes here: a file stands for the clipboard).
func TestMacClipboard(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board")
	copyBin, pasteBin := filepath.Join(dir, "pbcopy"), filepath.Join(dir, "pbpaste")
	_ = os.WriteFile(copyBin, []byte("#!/bin/sh\ncat > "+board+"\n"), 0o755)
	_ = os.WriteFile(pasteBin, []byte("#!/bin/sh\ncat "+board+"\n"), 0o755)
	oldCopy, oldPaste, oldOS := pbcopyPath, pbpastePath, installOS
	pbcopyPath, pbpastePath, installOS = copyBin, pasteBin, "darwin"
	t.Cleanup(func() { pbcopyPath, pbpastePath, installOS = oldCopy, oldPaste, oldOS })

	if _, err := macClipboard(context.Background(), json.RawMessage(`{"set": "从手机来的 · ok"}`)); err != nil {
		t.Fatal(err)
	}
	got, err := macClipboard(context.Background(), json.RawMessage(`{}`))
	if err != nil || got.(map[string]any)["text"] != "从手机来的 · ok" {
		t.Fatalf("%v %v", got, err)
	}
	installOS = "linux"
	if _, err := macClipboard(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("only a Mac")
	}
}

// pbcopy and pbpaste run in UTF-8 whatever the node's locale (#3075): under launchd there is none, and they took the
// text as MacRoman. The fakes write down the locale they were given.
func TestMacClipboardRunsInUTF8(t *testing.T) {
	dir := t.TempDir()
	seen := filepath.Join(dir, "seen")
	copyBin, pasteBin := filepath.Join(dir, "pbcopy"), filepath.Join(dir, "pbpaste")
	script := "#!/bin/sh\ncat > /dev/null\necho \"$LANG $LC_ALL $LC_CTYPE\" >> " + seen + "\n"
	_ = os.WriteFile(copyBin, []byte(script), 0o755)
	_ = os.WriteFile(pasteBin, []byte(script), 0o755)
	oldCopy, oldPaste, oldOS := pbcopyPath, pbpastePath, installOS
	pbcopyPath, pbpastePath, installOS = copyBin, pasteBin, "darwin"
	t.Cleanup(func() { pbcopyPath, pbpastePath, installOS = oldCopy, oldPaste, oldOS })
	t.Setenv("LANG", "C")
	t.Setenv("LC_ALL", "C")

	if _, err := macClipboard(context.Background(), json.RawMessage(`{"set": "中文"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := macClipboard(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(seen)
	want := "en_US.UTF-8 en_US.UTF-8 en_US.UTF-8\n"
	if string(got) != want+want {
		t.Fatalf("pbcopy and pbpaste saw %q", got)
	}
}

// Reads stop at the byte cap without cutting a UTF-8 rune; JSON would otherwise replace the partial rune with U+FFFD (#8070).
func TestMacClipboardTruncatesAtRuneBoundary(t *testing.T) {
	type testCase struct {
		name, text, want string
	}
	ascii := strings.Repeat("a", clipboardMost)
	tests := []testCase{
		{"exactly at cap", ascii, ascii},
		{"ASCII past cap", ascii + "tail", ascii},
	}
	for _, r := range []struct{ name, text string }{
		{"2-byte", "é"},
		{"3-byte", "中"},
		{"4-byte", "😀"},
	} {
		for split := 1; split < len(r.text); split++ {
			prefix := strings.Repeat("a", clipboardMost-split)
			tests = append(tests, testCase{fmt.Sprintf("%s split after byte %d", r.name, split), prefix + r.text + "tail", prefix})
		}
		prefix := strings.Repeat("a", clipboardMost-len(r.text))
		tests = append(tests, testCase{r.name + " ends at cap", prefix + r.text + "tail", prefix + r.text})
		tests = append(tests, testCase{r.name + " starts at cap", ascii + r.text + "tail", ascii})
	}

	tail := func(s string) string {
		return s[max(0, len(s)-12):]
	}
	for _, k := range tests {
		t.Run(k.name, func(t *testing.T) {
			dir := t.TempDir()
			pasteBin := filepath.Join(dir, "pbpaste")
			if err := os.WriteFile(pasteBin, []byte("#!/bin/sh\n/bin/cat \"$0.data\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(pasteBin+".data", []byte(k.text), 0o600); err != nil {
				t.Fatal(err)
			}
			oldPaste, oldOS := pbpastePath, installOS
			pbpastePath, installOS = pasteBin, "darwin"
			t.Cleanup(func() { pbpastePath, installOS = oldPaste, oldOS })

			result, err := macClipboard(context.Background(), json.RawMessage(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			text := result.(map[string]any)["text"].(string)
			if !utf8.ValidString(text) {
				t.Errorf("truncated text is invalid UTF-8; final bytes: % x", []byte(tail(text)))
			}

			// callBuiltin JSON-marshals this map. Check what the client receives too.
			wire, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(wire, &response); err != nil {
				t.Fatal(err)
			}
			if strings.ContainsRune(response.Text, utf8.RuneError) {
				t.Errorf("wire text contains U+FFFD; tail %q", tail(response.Text))
			}
			if response.Text != k.want {
				t.Errorf("wire text has %d bytes ending %q; want %d bytes ending %q", len(response.Text), tail(response.Text), len(k.want), tail(k.want))
			}
		})
	}
}

// The real pbcopy and pbpaste in a launchd agent's environment (no LANG; the login's text encoding, MacRoman): what
// lands on this Mac's clipboard, read as any app reads it (in UTF-8), is the text sent, Chinese, emoji and a shell's
// special characters included, and reading it back gives the same. pbcopy and pbpaste in MacRoman would round-trip
// the bytes while every other app saw ‰∏≠Êñá. PERSONA_REAL_CLIPBOARD=1 on a Mac: it uses this Mac's clipboard, and
// puts back the text that was on it.
func TestMacClipboardRealRoundTrip(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("PERSONA_REAL_CLIPBOARD") != "1" {
		t.Skip("PERSONA_REAL_CLIPBOARD=1 on a Mac")
	}
	utf8 := append(os.Environ(), "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8")
	oldOS := installOS
	installOS = "darwin"
	t.Cleanup(func() { installOS = oldOS })
	read := exec.Command(pbpastePath)
	read.Env = utf8
	before, _ := read.Output()
	t.Cleanup(func() {
		c := exec.Command(pbcopyPath)
		c.Env = utf8
		c.Stdin = strings.NewReader(string(before))
		_ = c.Run()
	})
	for _, k := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	t.Setenv("__CF_USER_TEXT_ENCODING", "0x1F5:0x0:0x0")
	text := "中文 · emoji 😀 · café\n$ \" ' \\ ` ! & | ;\ttab\r\nend"
	if _, err := macClipboard(context.Background(), mustJSON(map[string]any{"set": text})); err != nil {
		t.Fatal(err)
	}
	other := exec.Command(pbpastePath)
	other.Env = utf8
	if seen, err := other.Output(); err != nil || string(seen) != text {
		t.Fatalf("the Mac's clipboard has %q (%v)", seen, err)
	}
	got, err := macClipboard(context.Background(), json.RawMessage(`{}`))
	if err != nil || got.(map[string]any)["text"] != text {
		t.Fatalf("%q %v", got, err)
	}
}
