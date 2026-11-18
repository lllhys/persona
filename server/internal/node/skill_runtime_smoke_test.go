package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/prompts"
)

// Opt-in bridge for the real CLI smoke: discover and resolve through production code,
// then hand only the selected disposable file's instruction to the runtime harness.
func TestClaudeSkillRuntimeSmokeContract(t *testing.T) {
	cwd := os.Getenv("PERSONA_SKILL_SMOKE_DIR")
	if cwd == "" {
		t.Skip("requires the disposable real-runtime smoke harness")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal("cannot establish smoke home")
	}
	profile := os.Getenv("PERSONA_SKILL_SMOKE_PROFILE")
	if profile == "" {
		profile = "default"
	}
	n := &Node{home: home, dir: t.TempDir(), machineID: "skill-smoke", runtimes: &runtimeRegistry{}}
	ctx := withSkillOwner(context.Background(), "skill-smoke")
	request := skillCatalogRequest{Owner: "skill-smoke", Machine: n.machineID, Cwd: cwd,
		Launcher: "claude", ConfigDir: profile}
	catalog, err := n.skillCatalog(ctx, request)
	if err != nil {
		t.Fatal("effective Claude catalog unavailable; details redacted")
	}
	file := filepath.Join(cwd, ".claude", "skills", "persona-explicit-smoke", "SKILL.md")
	var reference string
	for _, entry := range catalog.Skills {
		if entry.Name == "persona-explicit-smoke" && entry.Path == file {
			reference = entry.ID
		}
	}
	if reference == "" {
		t.Fatal("disposable project skill missing from effective catalog")
	}
	text := `<persona-context kind="skill" ref="` + reference + `">project</persona-context>`
	selected, err := n.selectedSkills(ctx, "", cwd, "claude", text,
		modelExecution{ConfigDir: profile})
	if err != nil || len(selected) != 1 || selected[0].Path != file {
		t.Fatal("exact selected reference did not resolve")
	}
	instruction := prompts.RenderAssistantSelectedSkills([]prompts.AssistantSelectedSkill{
		{Name: selected[0].Name, Path: selected[0].Path, Ref: selected[0].ID},
	})
	payload, err := json.Marshal(map[string]string{"prompt": text + "\n" + instruction + "\nUse the selected skill for this smoke."})
	if err != nil || os.WriteFile(filepath.Join(cwd, "delivery.json"), payload, 0o600) != nil {
		t.Fatal("cannot hand disposable selection to smoke harness")
	}
}
