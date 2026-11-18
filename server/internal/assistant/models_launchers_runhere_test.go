package assistant

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A managed Codex launcher starts as the node's own Codex launchers do (they share codex-start): with no first message
// when theirs say prompt_optional, and in a pane the user picked when theirs say run_here (Run Agent Here, #5504).
func TestManagedCodexLauncherFollowsTheNodes(t *testing.T) {
	for _, tc := range []struct {
		name           string
		node           proto.Launcher
		optional, here bool
	}{
		{"fixed node", proto.Launcher{Name: "codex", Agent: "codex", PromptOptional: true, RunHere: true}, true, true},
		{"node without Run Agent Here for Codex", proto.Launcher{Name: "codex", Agent: "codex", PromptOptional: true}, true, false},
		{"older node", proto.Launcher{Name: "codex", Agent: "codex"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, _ := modelService(t)
			f.launcherList = []proto.Launcher{tc.node, {Name: "claude"}}
			if rec := request(s, "POST", "/api/v1/launchers", `{"name":"mycx","agent":"codex","base_url":"https://gw.example.com","model":"m"}`); rec.Code != 200 {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			got, err := s.machineLaunchers(context.Background(), "server")
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, l := range got {
				switch {
				case l.Name == "mycx":
					found = true
					if l.Agent != "codex" || l.PromptOptional != tc.optional || l.RunHere != tc.here {
						t.Errorf("managed %+v", l)
					}
				case l.Agent != "codex" && l.RunHere:
					t.Errorf("%s says run_here", l.Name)
				}
			}
			if !found {
				t.Fatalf("no managed Codex launcher in %+v", got)
			}
		})
	}
}
