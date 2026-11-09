package i18n

import (
	"os"
	"os/exec"
	"runtime"
	"slices"
	"testing"
)

// catalogs is every language's catalog, each parsed now if it was not: what the catalog tests read.
func catalogs() map[Lang]map[string]string {
	out := make(map[Lang]map[string]string, len(Langs))
	for _, l := range Langs {
		out[l] = catalog(l)
	}
	return out
}

// parsedLangs are the languages whose catalog this process parsed and keeps.
func parsedLangs() []Lang {
	var out []Lang
	for _, l := range Langs {
		if catalogOf[l].parsed.Load() {
			out = append(out, l)
		}
	}
	return out
}

const lazyCatalogChild = "PERSONA_TEST_I18N_LAZY_CHILD"

// A process that only speaks en parses en, and zh-Hans for a missing key, never ja: each language is about 0.4 MB
// once parsed. The reverse templates (InboxText) read the other languages without keeping them. Run in a process of
// its own, as other tests parse every language.
func TestCatalogsParsedPerLanguage(t *testing.T) {
	if os.Getenv(lazyCatalogChild) == "" {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), lazyCatalogChild+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return
	}
	if got := parsedLangs(); len(got) != 0 {
		t.Fatalf("parsed before any text: %v", got)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if got := T(En, "push.agent_asks", Args{"agent": "Codex"}); got != "Codex has a question" {
		t.Fatalf("en text: %q", got)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if got := parsedLangs(); !slices.Equal(got, []Lang{En}) {
		t.Fatalf("after an en text: %v parsed, want [en]", got)
	}
	t.Logf("en kept: %d bytes", after.HeapAlloc-before.HeapAlloc)
	if got := T(En, "no.such_key"); got != "no.such_key" || !Has("push.agent_asks") {
		t.Fatalf("missing key %q", got)
	}
	InboxText(En, "not a known label")
	if got := parsedLangs(); !slices.Equal(got, []Lang{ZhHans, En}) {
		t.Fatalf("after a missing key, Has and the inbox templates: %v parsed, want [zh-Hans en]", got)
	}
}
