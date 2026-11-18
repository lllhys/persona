package node

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestDirsRecentExcludesAssistantDirectories(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "custom-data")
	t.Setenv("PERSONA_HOME", root)
	n := &Node{home: home, metas: map[string]quickMeta{}, agents: map[string]host.Agent{}}
	newest := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for i, path := range []string{
		filepath.Join(home, ".persona", "assistant"),
		filepath.Join(home, ".persona", "assistant"),
		filepath.Join(root, "assistant"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		n.metas[fmt.Sprint(i)] = quickMeta{cwd: path, mtime: newest}
		n.agents[fmt.Sprint(i)] = host.Agent{Cwd: path}
	}
	alias := filepath.Join(home, "alias", "assistant")
	if err := os.MkdirAll(filepath.Dir(alias), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "assistant"), alias); err != nil {
		t.Fatal(err)
	}
	n.metas["alias"] = quickMeta{cwd: alias, mtime: newest}

	project := filepath.Join(home, "projects", "app")
	assistantProject := filepath.Join(home, "projects", "assistant")
	liveProject := filepath.Join(home, "projects", "live")
	for _, path := range []string{project, assistantProject, liveProject} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	n.metas["app"] = quickMeta{cwd: project, mtime: newest.Add(-time.Hour)}
	n.metas["app-older"] = quickMeta{cwd: project, mtime: newest.Add(-3 * time.Hour)}
	n.metas["assistant-project"] = quickMeta{cwd: assistantProject, mtime: newest.Add(-2 * time.Hour)}
	n.metas["missing"] = quickMeta{cwd: filepath.Join(home, "missing"), mtime: newest}
	n.agents["live"] = host.Agent{Cwd: liveProject}

	dirs, err := n.dirs("~")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{project, assistantProject, liveProject}
	if len(dirs.Recent) != len(want) {
		t.Fatalf("recent directories = %+v, want %v", dirs.Recent, want)
	}
	for i, path := range want {
		if dirs.Recent[i].Path != path {
			t.Errorf("recent[%d] = %q, want %q", i, dirs.Recent[i].Path, path)
		}
	}
	if !dirs.Recent[0].LastUsed.Equal(newest.Add(-time.Hour)) {
		t.Errorf("project last used = %v", dirs.Recent[0].LastUsed)
	}
	if len(n.metas) != 8 || len(n.agents) != 4 {
		t.Fatal("directory suggestions must preserve existing session metadata and agents")
	}
	if _, err := n.dirs(filepath.Join(root, "assistant")); err != nil {
		t.Fatalf("explicitly browsing the assistant directory: %v", err)
	}
}

func TestRecentDirsAppliesLimitAfterExcludingAssistant(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", filepath.Join(home, ".persona"))
	n := &Node{home: home, metas: map[string]quickMeta{}}
	for i := range 16 {
		path := filepath.Join(home, fmt.Sprintf("project-%02d", i))
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		n.metas[path] = quickMeta{cwd: path, mtime: time.Unix(int64(i+1), 0)}
	}
	assistant := filepath.Join(home, ".persona", "assistant")
	if err := os.MkdirAll(assistant, 0o700); err != nil {
		t.Fatal(err)
	}
	n.agents = map[string]host.Agent{"assistant": {Cwd: assistant}}
	n.metas[assistant] = quickMeta{cwd: assistant, mtime: time.Unix(100, 0)}
	got := n.recentDirs()
	if len(got) != 15 {
		t.Fatalf("got %d recent directories, want 15", len(got))
	}
	for i, dir := range got {
		want := filepath.Join(home, fmt.Sprintf("project-%02d", 15-i))
		if dir.Path != want {
			t.Errorf("recent[%d] = %q, want %q", i, dir.Path, want)
		}
	}
}

// A directory reached by two paths, as a home that is a symbolic link to another disk, is one entry: under the path it
// was used by last, with that time. A different directory of the same name and a worktree of the same repository are
// directories of their own (#8909).
func TestRecentDirsListsADirectoryOnceWhateverItsPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", filepath.Join(home, ".persona"))
	data := filepath.Join(home, "data")
	link := filepath.Join(home, "link") // the home's other spelling
	repo := filepath.Join(data, "projects", "demo")
	other := filepath.Join(data, "elsewhere", "demo")
	for _, dir := range []string{repo, other} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(data, link); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(link, "projects", "demo")
	newest := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	n := &Node{home: home, metas: map[string]quickMeta{
		"physical": {cwd: repo, mtime: newest.Add(-2 * time.Hour)},
		"alias":    {cwd: alias, mtime: newest},
		"other":    {cwd: other, mtime: newest.Add(-time.Hour)},
	}}
	want := []proto.RecentDir{
		{Path: alias, Name: "demo", LastUsed: newest},
		{Path: other, Name: "demo", LastUsed: newest.Add(-time.Hour)},
	}
	if git, err := exec.LookPath("git"); err == nil {
		worktree := filepath.Join(data, "worktrees", "demo")
		for _, args := range [][]string{
			{"-C", repo, "init", "-q"},
			{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "first"},
			{"-C", repo, "worktree", "add", "-q", worktree},
		} {
			if out, err := exec.Command(git, args...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v: %s", args, err, out)
			}
		}
		n.metas["worktree"] = quickMeta{cwd: worktree, mtime: newest.Add(-3 * time.Hour)}
		want = append(want, proto.RecentDir{Path: worktree, Name: "demo", LastUsed: newest.Add(-3 * time.Hour)})
	}
	if got := n.recentDirs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("recent directories:\n got %+v\nwant %+v", got, want)
	}

	// Used last under its physical path: listed under that one.
	n.metas["physical"] = quickMeta{cwd: repo, mtime: newest.Add(time.Hour)}
	if got := n.recentDirs(); len(got) == 0 || got[0].Path != repo || !got[0].LastUsed.Equal(newest.Add(time.Hour)) ||
		slices.ContainsFunc(got, func(d proto.RecentDir) bool { return d.Path == alias }) {
		t.Fatalf("after a run under the physical path: %+v", got)
	}
}

// The 15 entries are 15 directories: a second path to one already listed does not take a place.
func TestRecentDirsLimitCountsDirectoriesNotPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", filepath.Join(home, ".persona"))
	data := filepath.Join(home, "data")
	link := filepath.Join(home, "link")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(data, link); err != nil {
		t.Fatal(err)
	}
	n := &Node{home: home, metas: map[string]quickMeta{}}
	for i := range 16 {
		name := fmt.Sprintf("project-%02d", i)
		if err := os.MkdirAll(filepath.Join(data, name), 0o700); err != nil {
			t.Fatal(err)
		}
		n.metas["data-"+name] = quickMeta{cwd: filepath.Join(data, name), mtime: time.Unix(int64(10*i+1), 0)}
		n.metas["link-"+name] = quickMeta{cwd: filepath.Join(link, name), mtime: time.Unix(int64(10*i+2), 0)}
	}
	got := n.recentDirs()
	if len(got) != 15 {
		t.Fatalf("got %d recent directories, want 15: %+v", len(got), got)
	}
	for i, dir := range got {
		if want := filepath.Join(link, fmt.Sprintf("project-%02d", 15-i)); dir.Path != want {
			t.Errorf("recent[%d] = %q, want %q", i, dir.Path, want)
		}
	}
}
