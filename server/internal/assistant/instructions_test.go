package assistant

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// The CLAUDE.md of the assistant and of the health space are written on the assistant's machine right after the hub
// starts, again only when what they say changes, and not while the machine is offline (refreshInstructions).
func TestRefreshInstructions(t *testing.T) {
	ctx := context.Background()
	f := healthHub()
	f.devices[0].Online = true
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.MachineID = "server" })

	s.refreshInstructions(ctx)
	if len(f.prepared) != 2 || f.prepared[0]["dir"] != s.assistantRel() || f.prepared[1]["dir"] != s.healthRel() {
		t.Fatalf("after start: %v", f.prepared)
	}
	s.refreshInstructions(ctx)
	if len(f.prepared) != 2 {
		t.Fatalf("unchanged, written again: %d", len(f.prepared))
	}
	// What the machine has is not what this hub renders (an older hub's prompts): only that one is written.
	s.preparedMu.Lock()
	s.prepared["server\x00"+s.healthRel()] = preparedState{md: "old prompt"}
	s.preparedMu.Unlock()
	s.refreshInstructions(ctx)
	if len(f.prepared) != 3 || f.prepared[2]["dir"] != s.healthRel() {
		t.Fatalf("changed: %v", f.prepared[2:])
	}
	// A conversation started in between writes it too and counts as written.
	if _, err := s.prepare(ctx, "server"); err != nil {
		t.Fatal(err)
	}
	s.refreshInstructions(ctx)
	if len(f.prepared) != 4 {
		t.Fatalf("after prepare: %d", len(f.prepared))
	}

	off := healthHub() // Online false
	s = newTestService(t, off)
	setSettings(t, s, func(v *Settings) { v.MachineID = "server" })
	s.refreshInstructions(ctx)
	if len(off.prepared) != 0 {
		t.Fatalf("offline machine: %v", off.prepared)
	}
}

// The assistant's own space asks for the extensions' instructions with the digest of the hub's own part, so their data
// instructions can ride along when it changes; the health space asks without one and never gets them in its CLAUDE.md.
func TestPluginInstructionsBaseDigest(t *testing.T) {
	ctx := context.Background()
	f := healthHub()
	f.devices[0].Online = true
	var asked []string
	f.answerApp(AppPlugins, PluginsAssistant, func(params json.RawMessage) (any, error) {
		var a struct {
			Base string `json:"base_digest"`
		}
		_ = json.Unmarshal(params, &a)
		asked = append(asked, a.Base)
		return map[string]any{"instructions": "\n\n<!-- plugin notes (data) -->\nExtension words.\n", "skills": nil}, nil
	})
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.MachineID = "server" })
	s.refreshInstructions(ctx)
	if len(asked) != 2 || len(asked[0]) != 32 || asked[1] != "" {
		t.Fatalf("base digests asked with: %q", asked)
	}
	if len(f.prepared) != 2 {
		t.Fatalf("%v", f.prepared)
	}
	main, _ := f.prepared[0]["claude_md"].(string)
	health, _ := f.prepared[1]["claude_md"].(string)
	if !strings.HasSuffix(main, "Extension words.\n") || strings.Contains(health, "Extension words.") {
		t.Fatalf("main ends with the extension's part, health has none:\n%q\n%q", main[max(0, len(main)-80):], health)
	}
}

// skillsNode is the manager with one plugin skill and the assistant's machine it goes to. The manager sends a
// machine the skill's files once, and again after plugins.skills_need (extmgr extensionSkillsFor); the node keeps the
// files it gets and says which skills given without them it does not have (assistantskills.ApplyPlugins).
type skillsNode struct {
	digest   string           // the skill's digest on the hub now (an extension upgrade changes it)
	sent     string           // the digest whose files the manager last sent ("": none, or asked for again)
	has      string           // the digest whose files the node has ("": none)
	prepares []skillsPrepared // the node's assistant.prepare calls
}

// skillsPrepared is one assistant.prepare: its directory, and whether the skill came with its files.
type skillsPrepared struct {
	dir   string
	files bool
}

func newSkillsNode(f *fakeHub, digest string) *skillsNode {
	n := &skillsNode{digest: digest}
	f.answerApp(AppPlugins, PluginsAssistant, func(json.RawMessage) (any, error) {
		skill := map[string]any{"name": "notes", "plugin": "ext", "digest": n.digest}
		if n.sent != n.digest {
			n.sent = n.digest
			skill["files"] = map[string][]byte{"SKILL.md": []byte("notes " + n.digest)}
		}
		return map[string]any{"instructions": "", "skills": []any{skill}}, nil
	})
	f.answerApp(AppPlugins, PluginsSkillsNeed, func(params json.RawMessage) (any, error) {
		var a struct {
			Names []string `json:"names"`
		}
		_ = json.Unmarshal(params, &a)
		if slices.Contains(a.Names, "notes") {
			n.sent = ""
		}
		return map[string]any{}, nil
	})
	f.answerNode(DeviceAssistantPrepare, func(_ string, params json.RawMessage) (any, error) {
		var a struct {
			Dir    string `json:"dir"`
			Skills []struct {
				Name   string            `json:"name"`
				Digest string            `json:"digest"`
				Files  map[string][]byte `json:"files"`
			} `json:"plugin_skills"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		p, need := skillsPrepared{dir: a.Dir}, []string{}
		for _, sk := range a.Skills {
			switch {
			case sk.Files != nil:
				n.has, p.files = sk.Digest, true
			case sk.Digest != n.has:
				need = append(need, sk.Name)
			}
		}
		n.prepares = append(n.prepares, p)
		return map[string]any{"cwd": "/home/me/" + a.Dir, "plugin_skills_need": need}, nil
	})
	return n
}

// A plugin skill an extension upgrade changes reaches the assistant's machine on the next refresh though the
// CLAUDE.md stays the same: its files go to the node, and the health space's directory is prepared after them for
// its copy. The files used to be dropped, as the CLAUDE.md had not changed, and the machine kept the old skill.
func TestRefreshInstructionsSkillChanged(t *testing.T) {
	ctx := context.Background()
	f := healthHub()
	f.devices[0].Online = true
	n := newSkillsNode(f, "v1")
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.MachineID = "server" })
	mainDir, healthDir := s.assistantRel(), s.healthRel()

	s.refreshInstructions(ctx)
	s.refreshInstructions(ctx)
	if want := []skillsPrepared{{mainDir, true}, {healthDir, false}}; !slices.Equal(n.prepares, want) || n.has != "v1" {
		t.Fatalf("after start: %v, the node has %q", n.prepares, n.has)
	}
	n.digest = "v2"
	s.refreshInstructions(ctx)
	if want := []skillsPrepared{{mainDir, true}, {healthDir, false}}; !slices.Equal(n.prepares[2:], want) || n.has != "v2" {
		t.Fatalf("after the upgrade: %v, the node has %q", n.prepares[2:], n.has)
	}
	s.refreshInstructions(ctx)
	if len(n.prepares) != 4 {
		t.Fatalf("unchanged, prepared again: %v", n.prepares[4:])
	}
}

// A plugin skill the node says it does not have (plugin_skills_need) comes with the next refresh, whichever space's
// directory found it missing, and that directory is prepared again once the files are there. The files sent again
// used to be dropped by the next refresh too, as nothing in the CLAUDE.md had changed.
func TestRefreshInstructionsSkillsNeed(t *testing.T) {
	ctx := context.Background()
	f := healthHub()
	f.devices[0].Online = true
	n := newSkillsNode(f, "v1")
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.MachineID = "server" })
	mainDir, healthDir := s.assistantRel(), s.healthRel()
	s.refreshInstructions(ctx)

	for _, c := range []struct {
		space string
		want  []skillsPrepared // the conversation's start, then the refresh's
	}{
		{"", []skillsPrepared{{mainDir, false}, {mainDir, true}}},
		{SpaceHealth, []skillsPrepared{{healthDir, false}, {mainDir, true}, {healthDir, false}}},
	} {
		n.has = "" // the machine lost its plugin skills (a new data directory)
		from := len(n.prepares)
		// A conversation starts in the space, and the node says it lacks the skill.
		if _, err := s.prepareSpace(ctx, "server", c.space); err != nil {
			t.Fatal(err)
		}
		s.refreshInstructions(ctx)
		if got := n.prepares[from:]; !slices.Equal(got, c.want) || n.has != "v1" {
			t.Fatalf("space %q: %v, the node has %q", c.space, got, n.has)
		}
	}
}
