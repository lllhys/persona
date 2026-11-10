package terncli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// fake is a client whose tern answers out from the command's first word and records every call.
func fake(out map[string]string, errs map[string]error) (*Client, *[][]string) {
	var calls [][]string
	c := &Client{
		Bin: func() string { return "tern" },
		Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			calls = append(calls, args)
			return []byte(out[args[0]]), errs[args[0]]
		},
	}
	return c, &calls
}

func TestWindowGoesAfterTheFirstWord(t *testing.T) {
	c, calls := fake(nil, nil)
	ctx := context.Background()
	_ = c.Keys(ctx, "w2", "11", "enter", "esc")
	_ = c.RunCommand(ctx, "", "11", "claude --resume x")
	_ = c.Rename(ctx, "", "4", "api")
	_, _ = c.NewTab(ctx, "", "4", "/home/me")
	want := [][]string{
		{"send", "--window", "w2", "11", "keys", "enter", "esc"},
		{"run", "--window", "", "11", "claude --resume x"}, // one argument: nothing after it is typed
		{"rename", "--window", "", "4", "api"},
		{"new", "--window", "", "tab", "4", "--cwd", "/home/me", "--json"},
	}
	if len(*calls) != len(want) {
		t.Fatalf("calls %q", *calls)
	}
	for i, w := range want {
		if !slices.Equal((*calls)[i], w) {
			t.Errorf("call %d = %q, want %q", i, (*calls)[i], w)
		}
	}
}

func TestErrorsAreClassified(t *testing.T) {
	ctx := context.Background()
	c, _ := fake(nil, map[string]error{
		"inspect": fmt.Errorf("exit status 1: tern inspect: no Tern is running (no session daemon on /tmp/x.sock); start Tern"),
		"process": fmt.Errorf("exit status 1: tern process: no block is called `9`"),
	})
	if _, err := c.Inspect(ctx, ""); !errors.Is(err, ErrNotRunning) {
		t.Errorf("inspect: %v", err)
	}
	if _, err := c.Process(ctx, "", "9"); !errors.Is(err, ErrNotFound) {
		t.Errorf("process: %v", err)
	}
	none := &Client{Bin: func() string { return "" }}
	if _, err := none.Inspect(ctx, ""); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("not installed: %v", err)
	}
	if v := none.Version(ctx); v != "" {
		t.Errorf("version without tern = %q", v)
	}
}

func TestInspectAndVersion(t *testing.T) {
	c, _ := fake(map[string]string{
		"--version": "tern 0.9.13 (8c6dfbd)\n",
		"inspect": `{"sessions":[{"id":1563368095749,"name":"Default","shown":true,"tabs":[{"id":2,"name":null,"number":1,
			"blocks":[{"id":3,"program":"/bin/zsh","live":true,"browser":null},
			{"id":4,"program":"","live":false,"browser":"https://example.com"}]}]}],
			"panes":[{"pane":3,"program":"/bin/zsh","working":true,"quiet_ms":12}],
			"clients":[{"kind":"window","window":""},{"kind":"cli","window":""},{"kind":"window","window":"w2"},{"kind":"window","window":"w2"}]}`,
	}, nil)
	ctx := context.Background()
	if v := c.Version(ctx); v != "0.9.13" {
		t.Errorf("version %q", v)
	}
	in, err := c.Inspect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	s := in.Sessions[0]
	if s.ID != "1563368095749" || s.Tabs[0].Name != nil || !s.Tabs[0].Blocks[0].Terminal() || s.Tabs[0].Blocks[1].Terminal() {
		t.Errorf("session %+v", s)
	}
	if !in.Panes[0].Working || in.Panes[0].Pane != "3" {
		t.Errorf("panes %+v", in.Panes)
	}
	if w := in.Windows(); !slices.Equal(w, []string{"", "w2"}) {
		t.Errorf("windows %q", w)
	}
}

func TestEvents(t *testing.T) {
	lines := `{"event":"pane_spawned","pane":5,"by":2}
not json
{"event":"bell","pane":5}
`
	var args []string
	c := &Client{Bin: func() string { return "tern" }, Stream: func(_ context.Context, _ string, a ...string) (stream, error) {
		args = a
		return stream{out: bufio.NewScanner(strings.NewReader(lines)), wait: func() error {
			return fmt.Errorf("exit status 1: tern events: no Tern is running")
		}}, nil
	}}
	var got []string
	err := c.Events(context.Background(), []string{"pane_spawned", "bell"}, func() { got = append(got, "live") },
		func(ev Event) { got = append(got, ev.Event+":"+string(ev.Pane)) })
	if !slices.Equal(args, []string{"events", "--window", "", "--filter", "pane_spawned,bell"}) {
		t.Errorf("args %q", args)
	}
	if !slices.Equal(got, []string{"live", "pane_spawned:5", "bell:5"}) {
		t.Errorf("got %q", got)
	}
	if !errors.Is(err, ErrNotRunning) {
		t.Errorf("end: %v", err)
	}
}
