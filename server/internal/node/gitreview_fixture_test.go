package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// The answers git.review really sends, written to spec/fixtures/review for the clients to decode (#8733): a hunk with no
// function context has no "header", a repository with no commit yet a base with no "hash". iOS required both and could
// not read a real review while the mock, which always sent them, passed. Rewrite with
// go test ./internal/node -run '^TestGitReviewWireFixture$' -update.
func TestGitReviewWireFixture(t *testing.T) {
	type fixtureCase struct {
		Name   string          `json:"name"`
		Note   string          `json:"note"`
		Answer json.RawMessage `json:"answer"`
	}
	answer := func(res *proto.GitReview) json.RawMessage {
		t.Helper()
		normalizeReview(res)
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	// A branch: an edit at a file's first line (no line above it to name), one further down (named), a new file
	// committed on the branch and one Git does not track yet.
	notes := func(edits map[int]string) string {
		var b strings.Builder
		for i := 1; i <= 30; i++ {
			line, ok := edits[i]
			if !ok {
				line = fmt.Sprintf("item %d", i)
			}
			b.WriteString(line + "\n")
		}
		return b.String()
	}
	r := newReviewRepo(t, map[string]string{"notes.txt": notes(nil)})
	r.git("switch", "-q", "-c", "feat/notes")
	r.write("notes.txt", notes(map[int]string{1: "item one", 22: "item twenty-two"}))
	r.write("plan.md", "# Plan\n\n- one\n")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "Edit the notes")
	r.write("draft.txt", "not added yet\n")
	branch := r.review(proto.ReviewBranch, "")
	var named, unnamed int
	for _, f := range branch.Files {
		for _, h := range f.Hunks {
			if h.Header == "" {
				unnamed++
			} else {
				named++
			}
		}
	}
	if named == 0 || unnamed < 3 {
		t.Fatalf("want hunks with and without a header: %d named, %d not", named, unnamed)
	}

	// No commit yet: the base is empty, every file new.
	reviewGitEnv(t)
	dir := t.TempDir()
	runPullGit(t, dir, "init", "-q", "--initial-branch=main")
	fresh := &reviewRepo{t: t, node: &Node{home: dir, state: newState()}, dir: dir}
	fresh.write("hello.txt", "hello\n")
	first := fresh.review(proto.ReviewUncommitted, "")
	if !first.Base.Empty || first.Base.Hash != "" {
		t.Fatalf("no commit yet: %+v", first.Base)
	}

	doc := struct {
		Version int           `json:"version"`
		Source  string        `json:"source"`
		Cases   []fixtureCase `json:"cases"`
	}{1, "server/internal/node/gitreview_fixture_test.go: TestGitReviewWireFixture", []fixtureCase{
		{"branch_hunks_without_header", "A hunk with no line above it to name, a new file and an untracked one have no header key.", answer(branch)},
		{"no_commit_yet", "Before the first commit the base is {\"empty\":true}, with no hash, and there is no head.", answer(first)},
	}}
	got, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("..", "..", "..", "spec", "fixtures", "review", "answers.json")
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from what git.review answers; rewrite it with -update and read the diff:\n%s", path, got)
	}
}

// normalizeReview replaces what changes from run to run (the temporary folder, commit and blob ids) with placeholders.
func normalizeReview(res *proto.GitReview) {
	res.Root = "/home/dev/code/app"
	if res.Base.Hash != "" {
		res.Base.Hash, res.Base.Short = strings.Repeat("1", 40), "1111111"
	}
	if res.Head != nil {
		res.Head.Hash, res.Head.Short = strings.Repeat("2", 40), "2222222"
	}
	for i := range res.Files {
		if res.Files[i].Revision != "" {
			res.Files[i].Revision = fmt.Sprintf("%07d", i+1)
		}
	}
}
