package node

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// projectTranscripts lists what globProjects("*", "*.jsonl"), globProjects("*", "*", "subagents", "*.jsonl") and
// globProjects("*", "*", "subagents", "workflows", "*", "agent-*.jsonl") list, in the same order: every projects
// directory's transcripts, then every one's subagent transcripts, then the transcripts of its workflow runs' agents
// (workflows.go; the usage meter missed them until #9255). The usage meter asks for them every 10 seconds; globs read each
// projects directory once per pattern, and stat every transcript to learn it is not a directory. One walk reads each
// directory once and takes that from the directory entry (walkProjects).
func (n *Node) projectTranscripts() []string {
	var mains, subs, flows []string
	for _, p := range n.projectDirs() {
		if n.sessionScope != nil && !sessionScopeWithin(n.sessionScope.claude, p) {
			continue
		}
		var m, s, f []string
		if hasGlobMeta(p) { // the globs would read p as a pattern: let them
			m, _ = filepath.Glob(filepath.Join(p, "*", "*.jsonl"))
			s, _ = filepath.Glob(filepath.Join(p, "*", "*", "subagents", "*.jsonl"))
			f, _ = filepath.Glob(filepath.Join(p, "*", "*", "subagents", "workflows", "*", "agent-*.jsonl"))
		} else {
			m, s, f = walkProjects(filepath.Clean(p))
		}
		for _, path := range m {
			if n.scopedTranscript(path) {
				mains = append(mains, path)
			}
		}
		for _, path := range s {
			if n.scopedTranscript(path) {
				subs = append(subs, path)
			}
		}
		for _, path := range f {
			if n.scopedTranscript(path) {
				flows = append(flows, path)
			}
		}
	}
	return append(append(mains, subs...), flows...)
}

// walkProjects returns what filepath.Glob returns for root/*/*.jsonl (mains), root/*/*/subagents/*.jsonl (subs) and
// root/*/*/subagents/workflows/*/agent-*.jsonl (flows: a workflow run's agents), root holding no glob metacharacters:
// the same names in the same order (sorted per directory, as Glob reads them), any kind of file matching the last
// part. Glob stats each candidate directory; here a directory entry that is a regular file is passed over without one,
// a directory is read at once, and only anything else (a symlink, or a kind the system does not report) is stat'ed
// first, so a link reaches what Glob reaches through it.
func walkProjects(root string) (mains, subs, flows []string) {
	projects, _ := os.ReadDir(root)
	for _, project := range projects {
		dir := filepath.Join(root, project.Name())
		for _, e := range readDirFollowing(dir, project.Type()) {
			path := filepath.Join(dir, e.Name())
			if strings.HasSuffix(e.Name(), ".jsonl") {
				mains = append(mains, path)
			}
			for _, sub := range readDirFollowing(path, e.Type()) {
				if sub.Name() != "subagents" {
					continue
				}
				agents := filepath.Join(path, sub.Name())
				for _, a := range readDirFollowing(agents, sub.Type()) {
					if strings.HasSuffix(a.Name(), ".jsonl") {
						subs = append(subs, filepath.Join(agents, a.Name()))
					}
					if a.Name() != "workflows" {
						continue
					}
					runs := filepath.Join(agents, a.Name())
					for _, run := range readDirFollowing(runs, a.Type()) {
						dir := filepath.Join(runs, run.Name())
						for _, f := range readDirFollowing(dir, run.Type()) {
							if strings.HasPrefix(f.Name(), "agent-") && strings.HasSuffix(f.Name(), ".jsonl") {
								flows = append(flows, filepath.Join(dir, f.Name()))
							}
						}
					}
				}
			}
		}
	}
	return mains, subs, flows
}

// readDirFollowing reads path, a directory entry of type typ, if it is a directory or leads to one; Glob reads the same
// ones (os.Stat, then the directory). A regular file needs no stat to be passed over.
func readDirFollowing(path string, typ fs.FileMode) []os.DirEntry {
	switch {
	case typ.IsRegular():
		return nil
	case typ != fs.ModeDir:
		if st, err := os.Stat(path); err != nil || !st.IsDir() {
			return nil
		}
	}
	entries, _ := os.ReadDir(path) // what it read before an error, as Glob keeps the names it read
	return entries
}

// hasGlobMeta reports whether path holds a character filepath.Glob treats as a pattern (its hasMeta).
func hasGlobMeta(path string) bool {
	meta := `*?[\`
	if runtime.GOOS == "windows" {
		meta = `*?[`
	}
	return strings.ContainsAny(path, meta)
}
