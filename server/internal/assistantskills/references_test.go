package assistantskills

import (
	"strings"
	"testing"
)

func TestSelectedReferencesOnlyParseLeadingExplicitContexts(t *testing.T) {
	block := `<persona-context kind="skill" title="Review" url="" ref="opaque">Review · project</persona-context>`
	for _, text := range []string{"me@example.com @review", "literal @review", "quoted " + block, "> " + block} {
		refs, err := References(text)
		if err != nil || len(refs) != 0 {
			t.Fatalf("ordinary text became a selection: %q: %v %v", text, refs, err)
		}
	}
	refs, err := References(`<persona-context kind="workspace">Folder</persona-context>` + block + "\nCheck this")
	if err != nil || len(refs) != 1 || refs[0] != "opaque" {
		t.Fatalf("selected reference lost: %v %v", refs, err)
	}
	for _, attrs := range []string{`ref=""`, `ref="one" ref="two"`, `ref="one" path="/secret"`, `kind="context" ref="one"`} {
		text := `<persona-context kind="skill" ` + attrs + `>Review</persona-context>`
		if _, err := References(text); err == nil {
			t.Fatalf("accepted malformed selection: %s", text)
		}
	}
}

func TestResolveSelectionUsesCatalogIdentityNotClientPathOrName(t *testing.T) {
	first := Entry{Name: "review", Source: "project", Path: "/example/a/SKILL.md"}
	second := Entry{Name: "review", Source: "user", Path: "/example/b/SKILL.md"}
	catalog := Bind("assistant-a/machine/launcher/profile", []Entry{first, second})
	if catalog[0].ID == catalog[1].ID {
		t.Fatal("duplicate names lost identity")
	}
	text := `<persona-context kind="skill" ref="` + catalog[1].ID + `">untrusted display name</persona-context>`
	selected, err := Resolve(text, catalog)
	if err != nil || len(selected) != 1 || selected[0].Path != second.Path {
		t.Fatalf("wrong skill: %v %v", selected, err)
	}
	for _, changed := range [][]Entry{catalog[:1], Bind("assistant-b/machine/launcher/profile", []Entry{first, second}), Bind("assistant-a/machine/other/profile", []Entry{first, second})} {
		if _, err := Resolve(text, changed); err == nil {
			t.Fatal("stale or cross-environment selection silently resolved")
		}
	}
	if _, err := Resolve(strings.ReplaceAll(text, catalog[1].ID, "/secret"), catalog); err == nil {
		t.Fatal("accepted a client path")
	}
}
