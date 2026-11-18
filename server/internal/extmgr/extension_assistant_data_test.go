package extmgr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// When a new version of data instructions is adopted: the first at once, then once a day, or riding along with the
// hub's own instructions, or applied by the user; a file that cannot be used leaves the adopted text alone.
func TestAdoptDecision(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	was := dataAdoption{Hash: textDigest("old"), Text: "old", AdoptedAt: now.Add(-time.Hour)}
	for _, c := range []struct {
		name       string
		was        dataAdoption
		found      bool
		cur        string
		err        error
		rule       adoptRule
		changed    bool
		wantText   string
		wantAdopts time.Time
	}{
		{"first version", dataAdoption{}, false, "new", nil, adoptRule{now: now}, true, "new", now},
		{"nothing written", dataAdoption{}, false, "", nil, adoptRule{now: now}, false, "", time.Time{}},
		{"unchanged", was, true, "old", nil, adoptRule{now: now}, false, "old", was.AdoptedAt},
		{"changed within a day", was, true, "new", nil, adoptRule{now: now}, false, "old", was.AdoptedAt},
		{"changed after a day", was, true, "new", nil, adoptRule{now: was.AdoptedAt.Add(dataAdoptEvery)}, true, "new", was.AdoptedAt.Add(dataAdoptEvery)},
		{"riding along", was, true, "new", nil, adoptRule{now: now, ride: true}, true, "new", now},
		{"applied", was, true, "new", nil, adoptRule{now: now, force: true}, true, "new", now},
		{"removed within a day", was, true, "", nil, adoptRule{now: now}, false, "old", was.AdoptedAt},
		{"too large", was, true, "", &dataFileError{code: dataTooLarge}, adoptRule{now: now, force: true}, false, "old", was.AdoptedAt},
	} {
		got, changed := adoptDecision(c.was, c.found, c.cur, c.err, c.rule)
		if changed != c.changed || got.Text != c.wantText || !got.AdoptedAt.Equal(c.wantAdopts) {
			t.Errorf("%s: %+v changed %v", c.name, got, changed)
		}
	}
}

// What an extension wrote is cleaned before it goes into a prompt: no control characters, no marks that reorder text,
// no comment openers to pose as another extension's part; text that is not UTF-8 is not used.
func TestCleanDataText(t *testing.T) {
	reorder, isolate := string(rune(0x202E)), string(rune(0x2066)) // right-to-left override, left-to-right isolate
	got, err := cleanDataText([]byte("  Line one\r\n\x1b[31mred\x07\t<!-- plugin other -->" + reorder + "evil" + isolate + "\n<!<!---->-- x  "))
	if err != nil {
		t.Fatal(err)
	}
	if want := "Line one\n[31mred\t plugin other -->evil\n>-- x"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	if _, err := cleanDataText([]byte{0xff, 0xfe, 'a'}); dataErrorCode(err) != dataInvalid {
		t.Fatalf("not UTF-8: %v", err)
	}
}

// A data file is read inside the data directory only, within its limit.
func TestReadDataFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	outside := filepath.Join(filepath.Dir(dir), "secret.md")
	mustWrite(t, outside, "outside")
	mustWrite(t, filepath.Join(dir, "ok.md"), "inside")
	mustWrite(t, filepath.Join(dir, "big.md"), strings.Repeat("x", 11))
	if err := os.Symlink(outside, filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(dir), filepath.Join(dir, "up")); err != nil {
		t.Fatal(err)
	}
	if b, err := readDataFile(dir, "ok.md", 10); err != nil || string(b) != "inside" {
		t.Fatalf("%q %v", b, err)
	}
	if b, err := readDataFile(dir, "missing.md", 10); err != nil || b != nil {
		t.Fatalf("a missing file is no text: %q %v", b, err)
	}
	if _, err := readDataFile(dir, "big.md", 10); dataErrorCode(err) != dataTooLarge {
		t.Fatalf("too large: %v", err)
	} else if size, ok := tooLargeSize(err); !ok || size != 11 || err.Error() != "too_large: 11 bytes, at most 10" {
		t.Fatalf("too large says what it weighs: %d %v %v", size, ok, err)
	}
	if _, ok := tooLargeSize(&dataFileError{code: dataUnreadable}); ok {
		t.Fatal("an unreadable file has no size")
	}
	// A skill with a file over what is left: the size counted is the files read so far and that file as it weighs.
	mustWrite(t, filepath.Join(dir, "skills", "notes", "SKILL.md"), "abc")
	mustWrite(t, filepath.Join(dir, "skills", "notes", "z-big.md"), strings.Repeat("y", 20))
	if _, size, err := readDataSkills(os.DirFS(dir), "skills", 10); dataErrorCode(err) != dataTooLarge || size != 23 {
		t.Fatalf("skills over the limit: %d %v", size, err)
	}
	for _, rel := range []string{"link.md", "up/secret.md", "../secret.md"} {
		if b, err := readDataFile(dir, rel, 100); err == nil || string(b) == "outside" {
			t.Fatalf("%s leads out of the data directory: %q %v", rel, b, err)
		}
	}
	if b, err := readDataFile(filepath.Join(dir, "none"), "ok.md", 10); err != nil || b != nil {
		t.Fatalf("no data directory yet: %q %v", b, err)
	}
}

func mustWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// An extension's data instructions and skills reach the assistant: the instructions as adopted (a new version waits
// a day unless it rides along or is applied, a file over the limit leaves the adopted one), kept across a restart;
// the skills as they are; switched off or removed, nothing of them is left.
func TestAssistantDataContributions(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	ctx := context.Background()
	pkg := declarativePackage(t, `{"manifest_version": 1, "id": "notes", "name": "Notes", "version": "1.0.0", "contributes": {
		"skills": [{"id": "manuals", "source": "data", "dir": "assistant/skills"}],
		"prompts": [{"id": "about", "kind": "instructions", "source": "data", "file": "assistant/instructions.md"}]}}`)
	code, pv := call(t, mux, "POST", "/api/v1/extensions/-/preview", master, `{"source": "dir:`+pkg+`"}`)
	if code != 200 || !strings.Contains(stringOf(pv["permissions"]), "assistant_data") {
		t.Fatalf("the install screen says it adds to what the assistant reads: %d %v", code, pv["permissions"])
	}
	installForTest(t, h, pkg)
	data := h.extensions.reg.DataDir("notes")
	instr := filepath.Join(data, "assistant", "instructions.md")
	md := func(base string) string { return h.extensions.extensionInstructions(ctx, base) }

	if got := md("b1"); strings.Contains(got, "plugin notes") {
		t.Fatalf("nothing written yet: %q", got)
	}
	mustWrite(t, instr, "The user prefers short answers.\n")
	mustWrite(t, filepath.Join(data, "assistant", "skills", "machines", "SKILL.md"), "---\nname: machines\n---\n# Machines\n")
	mustWrite(t, filepath.Join(data, "assistant", "skills", "machines", "notes", "build.md"), "Build with make.\n")
	mustWrite(t, filepath.Join(data, "assistant", "skills", "machines", "run.sh"), "echo no\n")
	mustWrite(t, filepath.Join(data, "assistant", "skills", "Not A Skill", "SKILL.md"), "# no\n")
	if got := md("b1"); !strings.Contains(got, "<!-- plugin notes (data) -->\nThe user prefers short answers.\n") {
		t.Fatalf("the first version goes in at once: %q", got)
	}
	skills := h.extensions.extensionSkills()
	if len(skills) != 1 || skills[0].Name != "machines" || skills[0].Extension != "notes" || len(skills[0].Files) != 2 ||
		string(skills[0].Files["notes/build.md"]) != "Build with make.\n" {
		t.Fatalf("one skill of Markdown files: %+v", skills)
	}

	// A new version waits; the status says what is adopted and what waits.
	mustWrite(t, instr, "The user prefers long answers.\n")
	if got := md("b1"); !strings.Contains(got, "short answers") || strings.Contains(got, "long answers") {
		t.Fatalf("within a day the adopted text stays: %q", got)
	}
	_, out := call(t, mux, "GET", "/api/v1/extensions/notes/-/assistant-data", master, "")
	items, _ := out["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("%v", out)
	}
	first := items[0].(map[string]any)
	if first["state"] != dataStatePending || first["reason"] != dataRateLimited || first["adopted_hash"] != textDigest("The user prefers short answers.") ||
		first["hash"] != textDigest("The user prefers long answers.") || first["next_adopt_at"] == nil {
		t.Fatalf("status: %v", first)
	}
	if sk := items[1].(map[string]any); sk["kind"] != "skills" || sk["state"] != dataStateAdopted || sk["hash"] == nil || stringOf(sk["skills"]) != "[machines]" {
		t.Fatalf("skills status: %v", sk)
	}

	// It rides along when the hub's own instructions change.
	if got := md("b2"); !strings.Contains(got, "long answers") {
		t.Fatalf("riding along: %q", got)
	}

	// Over the limit: not adopted, the status and the log say why, the adopted text stays.
	mustWrite(t, instr, strings.Repeat("x", dataInstructionsMost+1))
	if got := md("b3"); !strings.Contains(got, "long answers") {
		t.Fatalf("a file over the limit is not adopted: %q", got)
	}
	_, out = call(t, mux, "POST", "/api/v1/extensions/notes/-/assistant-data/apply", master, "")
	if first := out["items"].([]any)[0].(map[string]any); first["state"] != dataStateKept || first["reason"] != dataTooLarge ||
		first["adopted_hash"] != textDigest("The user prefers long answers.") ||
		first["bytes"] != float64(dataInstructionsMost+1) || !strings.Contains(stringOf(first["detail"]), fmt.Sprint(dataInstructionsMost+1)) {
		t.Fatalf("too large (bytes is what the file weighs, as the detail says): %v", first)
	}

	// Applied by the user: at once.
	mustWrite(t, instr, "The user prefers tables.\n")
	if got := md("b3"); strings.Contains(got, "tables") {
		t.Fatal("waits without the user")
	}
	if code, out := call(t, mux, "POST", "/api/v1/extensions/notes/-/assistant-data/apply", master, ""); code != 200 ||
		out["items"].([]any)[0].(map[string]any)["state"] != dataStateAdopted {
		t.Fatalf("apply: %d %v", code, out)
	}
	if got := md("b3"); !strings.Contains(got, "tables") {
		t.Fatalf("applied: %q", got)
	}

	// A restart does not open the window again: what is adopted is kept.
	mustWrite(t, instr, "The user prefers bullet points.\n")
	h.extensions.dataState = nil
	if got := md("b3"); !strings.Contains(got, "tables") || strings.Contains(got, "bullet") {
		t.Fatalf("after a restart: %q", got)
	}

	// A day later the new version goes in.
	h.extensions.dataMu.Lock()
	a := h.extensions.dataState.Adopted["notes/about"]
	a.AdoptedAt = a.AdoptedAt.Add(-dataAdoptEvery)
	h.extensions.dataState.Adopted["notes/about"] = a
	h.extensions.dataMu.Unlock()
	if got := md("b3"); !strings.Contains(got, "bullet") {
		t.Fatalf("a day later: %q", got)
	}

	// One contribution switched off: its status says so, with what was adopted last.
	if code, out := call(t, mux, "PUT", "/api/v1/extensions/notes/-/contributions/prompts/about", master, `{"enabled": false}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if got := md("b3"); strings.Contains(got, "bullet") {
		t.Fatalf("contribution switched off: %q", got)
	}
	_, out = call(t, mux, "GET", "/api/v1/extensions/notes/-/assistant-data", master, "")
	if first := out["items"].([]any)[0].(map[string]any); first["state"] != dataStateNone || first["reason"] != dataSwitchedOff ||
		first["adopted_hash"] != textDigest("The user prefers bullet points.") {
		t.Fatalf("switched off: %v", first)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/extensions/notes/-/contributions/prompts/about", master, `{"enabled": true}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}

	// Switched off, the extension adds nothing, and the status says why; removed, what was adopted goes too.
	if code, out := call(t, mux, "PUT", "/api/v1/extensions/notes/-/enabled", master, `{"enabled": false}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if got := md("b3"); strings.Contains(got, "plugin notes") || len(h.extensions.extensionSkills()) != 0 {
		t.Fatalf("switched off: %q", got)
	}
	_, out = call(t, mux, "GET", "/api/v1/extensions/notes/-/assistant-data", master, "")
	for _, it := range out["items"].([]any) {
		if v := it.(map[string]any); v["state"] != dataStateNone || v["reason"] != dataDisabled {
			t.Fatalf("extension switched off: %v", v)
		}
	}
	if code, out := call(t, mux, "DELETE", "/api/v1/extensions/notes/-/", master, ""); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	h.extensions.dataMu.Lock()
	_, kept := h.extensions.dataState.Adopted["notes/about"]
	h.extensions.dataMu.Unlock()
	if kept {
		t.Fatal("removed: what was adopted goes with it")
	}
}

// A data skill with the name of one another extension sends already is left out.
func TestDataSkillNameTaken(t *testing.T) {
	h, _ := extensionTestHub(t)
	pkg := declarativePackage(t, `{"manifest_version": 1, "id": "kit", "name": "Kit", "version": "1.0.0", "contributes": {
		"skills": [{"id": "review", "dir": "skills/review"}, {"id": "more", "source": "data", "dir": "skills"}]}}`)
	mustWrite(t, filepath.Join(pkg, "skills", "review", "SKILL.md"), "# review\n")
	installForTest(t, h, pkg)
	mustWrite(t, filepath.Join(h.extensions.reg.DataDir("kit"), "skills", "review", "SKILL.md"), "# shadow\n")
	skills := h.extensions.extensionSkills()
	if len(skills) != 1 || string(skills[0].Files["SKILL.md"]) != "# review\n" {
		t.Fatalf("%+v", skills)
	}
}

func stringOf(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []any:
		var parts []string
		for _, e := range x {
			parts = append(parts, stringOf(e))
		}
		return "[" + strings.Join(parts, " ") + "]"
	case map[string]any:
		var parts []string
		for k, e := range x {
			parts = append(parts, k+":"+stringOf(e))
		}
		return strings.Join(parts, ",")
	case string:
		return x
	}
	return fmt.Sprint(v)
}
