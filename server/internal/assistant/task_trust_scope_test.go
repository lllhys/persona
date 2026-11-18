package assistant

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// A remembered kind covers its directory's tree on its machine with its access, however the directory is spelled
// (#8095); a too-broad directory covers only itself, and nothing covers a sibling whose name only starts the same.
func TestTrustCoversTreeSpellingsAndBreadth(t *testing.T) {
	homes := map[string]trustHome{"box": {home: "/home/me", real: "/data/home/me"}, "mac": {home: "/Users/me"}}
	have := map[string]bool{
		"box|/home/me/src/tool|edit":    true,
		"box|/home/me|edit":             true, // remembered on a card before #8095: the home itself, exact only
		"box|/srv|edit":                 true, // right under /: exact only
		"box|/data/home/me/notes/|edit": true, // an older key in the resolved spelling, with a trailing slash
		"mac|~/work|read":               true,
		"box|C:\\Users\\me|edit":        true,
	}
	for _, tc := range []struct {
		key  string
		want bool
	}{
		{"box|/home/me/src/tool|edit", true},
		{"box|/home/me/src/tool/cli|edit", true},
		{"box|/home/me/src/tool/cli/deep/er|edit", true},
		{"box|/home/me//src/tool/|edit", true},
		{"box|/home/me/src/./tool/../tool/cli|edit", true},
		{"box|/data/home/me/src/tool|edit", true},
		{"box|/data/home/me/src/tool/cli|edit", true},
		{"box|~/src/tool/cli|edit", true},
		{"box|/home/me/notes/today|edit", true},
		{"box|/home/me/src/toolbox|edit", false},
		{"box|/home/me/src/other|edit", false},
		{"box|/home/me/src|edit", false},
		{"box|/home/me|edit", true},
		{"box|/data/home/me|edit", true},
		{"box|/home/me/anything|edit", false},
		{"box|/srv|edit", true},
		{"box|/srv/app|edit", false},
		{"box|/home/me/src/tool/cli|read", false},
		{"other|/home/me/src/tool/cli|edit", false},
		{"mac|/Users/me/work/app|read", true},
		{"mac|/Users/me/work/app|edit", false},
		{"box|C:\\Users\\me|edit", true},
		{"box|C:\\Users\\me\\x|edit", false},
	} {
		if got := trustCovers(have, homes, tc.key); got != tc.want {
			t.Errorf("trustCovers(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
	// No home reported (an older hub's list, an offline machine never seen): only the lexical forms meet.
	if !trustCovers(have, nil, "box|/home/me/src/tool/cli|edit") || trustCovers(have, nil, "box|/data/home/me/src/tool|edit") {
		t.Error("without homes: the tree should match lexically and the resolved spelling should not")
	}
	for dir, want := range map[string]bool{"/": false, "/home": false, "/home/me": false, "/data/home/me": false,
		"/home/me/src": true, "/opt/repo": true, "/home/me/*": false} {
		if got := trustTreeDir(trustDir(dir, homes["box"]), homes["box"]); got != want {
			t.Errorf("trustTreeDir(%q) = %v, want %v", dir, got, want)
		}
	}
}

func trustFixture(t *testing.T) (*Service, *fakeHub) {
	t.Helper()
	f := newFakeHub().withMachine()
	f.devices = []Device{{ID: "box", Name: "Box", Home: "/home/me", HomeReal: "/data/home/me", Online: true, Reachable: true},
		{ID: "server", Name: "Server", Home: "/root", Online: true, Reachable: true}}
	return newServiceOn(t, f, &memStore{}), f
}

// The plan card's "Start these directly" on a task in a folder lets the next writable tasks in that folder's subfolders
// and in its other spellings start without a card; one elsewhere still waits for Start.
func TestPlanStartsAtOnceInARememberedFolderTree(t *testing.T) {
	ctx := context.Background()
	s, _ := trustFixture(t)
	edit := func(cwd string) PlanTask { return PlanTask{Machine: "box", Cwd: cwd} }
	if s.planStartsAtOnce(ctx, []PlanTask{edit("/home/me/src/tool")}) {
		t.Fatal("nothing remembered yet, the plan started")
	}
	s.trustKinds(ctx, []PlanTask{edit("/home/me/src/tool")}) // as startPlan with remember
	for _, cwd := range []string{"/home/me/src/tool", "/home/me/src/tool/cli", "/data/home/me/src/tool", "/data/home/me/src/tool/cli/x", "/home/me/src/tool/"} {
		if !s.planStartsAtOnce(ctx, []PlanTask{edit(cwd)}) {
			t.Errorf("%s: waits for Start, want it to start", cwd)
		}
	}
	for _, task := range []PlanTask{edit("/home/me/src/other"), edit("/home/me/src/toolbox"), {Machine: "server", Cwd: "/home/me/src/tool"},
		edit("/home/me/src")} {
		if s.planStartsAtOnce(ctx, []PlanTask{task}) {
			t.Errorf("%s on %s: started, want a card", task.Cwd, task.Machine)
		}
	}
	// One task outside the tree keeps the whole plan on its card.
	if s.planStartsAtOnce(ctx, []PlanTask{edit("/home/me/src/tool/cli"), edit("/home/me/src/other")}) {
		t.Error("a plan with a task outside the remembered tree started")
	}
	// The home itself remembered on a card covers only the home.
	s.trustKinds(ctx, []PlanTask{edit("/home/me")})
	if !s.planStartsAtOnce(ctx, []PlanTask{edit("/data/home/me")}) || s.planStartsAtOnce(ctx, []PlanTask{edit("/home/me/src/other")}) {
		t.Error("the remembered home should cover itself (in either spelling) and nothing under it")
	}
}

// persona_settings tasks.trust: get lists the kinds, add and remove change one once the owner approves on the phone, a
// too-broad folder is refused, and what is covered already asks nothing.
func TestTaskTrustSettingsTool(t *testing.T) {
	ctx := context.Background()
	s, _ := trustFixture(t)
	tool := func(args string) (string, bool) {
		r := s.Tool(ctx, ToolCall{Name: "persona_settings", Arguments: json.RawMessage(args), MachineID: "server", SessionID: "c1"})
		return r.Content[0].Text, r.IsError
	}
	change := func(action, value string) (string, bool) {
		return tool(`{"action":"` + action + `","key":"tasks.trust","value":` + value + `}`)
	}
	approve := func(card string) {
		go func() {
			q, ok := awaitCard(s)
			if !ok || q.Summary != card {
				t.Errorf("card %+v, want %q", q, card)
				return
			}
			allow := true
			raw, _ := json.Marshal(proto.Resolution{Allow: &allow})
			if _, err := s.Action(ctx, Action{Op: "answer", InteractionID: q.ID, Answer: raw}); err != nil {
				t.Error(err)
			}
		}()
	}
	if out, _ := tool(`{"action":"list"}`); !strings.Contains(out, "- tasks.trust (") {
		t.Fatalf("list does not offer tasks.trust:\n%s", out)
	}
	if out, isErr := tool(`{"action":"get","key":"tasks.trust"}`); isErr || !strings.Contains(out, "No kind of task is remembered") {
		t.Fatalf("get, empty: %s", out)
	}
	for _, bad := range []string{`{"machine":"box","directory":"/"}`, `{"machine":"box","directory":"~"}`, `{"machine":"box","directory":"/data/home/me"}`,
		`{"machine":"box","directory":"/home"}`, `{"machine":"box","directory":"src/tool"}`, `{"machine":"nope","directory":"/opt/x"}`,
		`{"machine":"box","directory":"/opt/x","access":"write"}`, `"box"`} {
		if out, isErr := change("add", bad); !isErr || len(s.pendingQuestions()) != 0 {
			t.Fatalf("add %s: %s (error %v)", bad, out, isErr)
		}
	}
	approve(i18n.T(i18n.Default, "assistant-settings_tool.change_task_trust_add_edit", i18n.Args{"machine": "Box", "directory": "/home/me/src/tools"}))
	if out, isErr := change("add", `{"machine":"Box","directory":"~/src//tools/"}`); isErr || !strings.Contains(out, "Remembered") {
		t.Fatalf("add approved: %s", out)
	}
	if got := storedTrust(t, s.store().(*memStore)); !slices.Equal(got, []string{"box|/home/me/src/tools|edit"}) {
		t.Fatalf("tasks.trust = %v", got)
	}
	if !s.planStartsAtOnce(ctx, []PlanTask{{Machine: "box", Cwd: "/data/home/me/src/tools/offline-a"}}) {
		t.Fatal("a writable task under the added folder (resolved home spelling) waits for Start")
	}
	if out, _ := tool(`{"action":"get","key":"tasks.trust"}`); !strings.Contains(out, "box /home/me/src/tools (and every directory under it), edit") {
		t.Fatalf("get: %s", out)
	}
	if out, isErr := change("add", `{"machine":"box","directory":"/data/home/me/src/tools/a"}`); isErr || !strings.Contains(out, "Already remembered") || len(s.pendingQuestions()) != 0 {
		t.Fatalf("add under a remembered folder: %s", out)
	}
	if out, isErr := change("set", `{"machine":"box","directory":"/opt/x"}`); !isErr {
		t.Fatalf("set is not an action of tasks.trust: %s", out)
	}
	approve(i18n.T(i18n.Default, "assistant-settings_tool.change_task_trust_remove_edit", i18n.Args{"machine": "Box", "directory": "/home/me/src/tools"}))
	if out, isErr := change("remove", `{"machine":"box","directory":"/data/home/me/src/tools"}`); isErr || !strings.Contains(out, "Forgotten") {
		t.Fatalf("remove approved: %s", out)
	}
	if got := storedTrust(t, s.store().(*memStore)); len(got) != 0 {
		t.Fatalf("tasks.trust after remove = %v", got)
	}
	if out, isErr := change("remove", `{"machine":"box","directory":"/home/me/src/tools"}`); isErr || !strings.Contains(out, "Not remembered") || len(s.pendingQuestions()) != 0 {
		t.Fatalf("remove what is not there: %s", out)
	}
}
