package identity

import (
	"runtime"
	"testing"
)

// A Windows node's directories read the same on a hub of any system: on Linux and macOS filepath splits only at /,
// and C:\Users\me\.persona\assistant was one name there (#6419).
func TestWindowsNodeDirectoriesOnAnyHub(t *testing.T) {
	for _, c := range []struct {
		path      string
		assistant bool
		space     string
		health    bool
	}{
		{`C:\Users\me\.persona\assistant`, true, "", false},
		{`c:\Users\me\.persona\assistant\`, true, "", false},
		{`C:/Users/me/.persona/assistant`, true, "", false},
		{`\\server\share\me\.persona\assistant`, true, "", false},
		{`C:\Users\me\.persona\assistant-m-0a1b2c`, true, "", false},
		{`C:\Users\me\.persona\health`, true, SpaceHealth, true},
		{`C:\Users\me\.persona\health\reports`, false, "", true},
		{`C:\Users\me\.persona\assistant-bin`, false, "", false},
		{`C:\Users\me\.persona\assistant\.claude\skills\health`, false, "", false},
		{`C:\work\repo`, false, "", false},
	} {
		if got := IsAssistantDir(c.path); got != c.assistant {
			t.Errorf("IsAssistantDir(%q) = %v, want %v", c.path, got, c.assistant)
		}
		if got := SpaceOf(c.path); got != c.space {
			t.Errorf("SpaceOf(%q) = %q, want %q", c.path, got, c.space)
		}
		if got := InHealthSpace(c.path); got != c.health {
			t.Errorf("InHealthSpace(%q) = %v, want %v", c.path, got, c.health)
		}
	}
	if !IsAssistantDirOf(`C:\Users\me\.persona\assistant-m-0a1b2c`, "m-0a1b2c") || IsAssistantDirOf(`C:\Users\me\.persona\assistant`, "m-0a1b2c") {
		t.Error("IsAssistantDirOf does not tell managers' directories apart in a Windows path")
	}
}

func TestNodePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		if got := NodePath(`C:\Users\me`); got != `C:\Users\me` {
			t.Fatalf("NodePath on Windows = %q, want the path as it is", got)
		}
		return
	}
	for in, want := range map[string]string{
		`C:\Users\me\.persona`: "C:/Users/me/.persona",
		`d:\x`:                 "d:/x",
		`\\server\share\x`:     "//server/share/x",
		`/home/me/a\b`:         `/home/me/a\b`, // a name with a backslash in it, on Linux or macOS
		`ab\c`:                 `ab\c`,
		"/home/me/.persona":    "/home/me/.persona",
		"":                     "",
	} {
		if got := NodePath(in); got != want {
			t.Errorf("NodePath(%q) = %q, want %q", in, got, want)
		}
	}
}
