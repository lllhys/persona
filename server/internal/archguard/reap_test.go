package archguard

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	reapCall  = "osproc.ReapInherited"
	serverDir = "../.." // the server module, from this package
)

// reapSite is a production function that reaps what an earlier image of its process left running
// (osproc.ReapInherited). The contract (osproc/reap_unix.go): call it first thing, before the process starts a child
// of its own, or the reaper takes that child and its exec.Cmd.Wait fails.
type reapSite struct {
	file   string            // under server/
	fn     string            // the function (or method) that calls it
	reaps  int               // how many osproc.ReapInherited calls it has
	before map[string]string // the calls allowed to run ahead of one, each with why it starts no process
}

var reapSites = []reapSite{
	{"internal/hub/listen.go", "claimInherited", 1, map[string]string{
		"inheritedListeners":   "takes the sockets systemd passed, once",
		"osproc.SealInherited": "marks inherited descriptors close-on-exec",
	}},
	{"internal/manager/run.go", "runManager", 1, map[string]string{
		"osproc.SealInherited": "marks inherited descriptors close-on-exec",
	}},
	{"internal/managercli/managercli.go", "Assistant", 1, map[string]string{
		"len":                  "builtin",
		"osproc.SealInherited": "marks inherited descriptors close-on-exec",
	}},
	{"internal/nodecmd/nodecmd.go", "runNode", 2, map[string]string{
		"len":                  "builtin",
		"IsHelpArg":            "compares an argument",
		"residentArgs":         "parses arguments",
		"checkNodeStdioArgs":   "parses arguments",
		"exitNodeUsage":        "prints the usage and exits",
		"osproc.SealInherited": "marks inherited descriptors close-on-exec",
		"procs.AtStart":        "sets GOMAXPROCS",
		"node.Relay":           "only dials a resident node's socket",
	}},
}

// hubClaimers are the hub's entry points: each calls claimInherited (and with it osproc.ReapInherited) first.
var hubClaimers = []struct{ file, fn string }{
	{"internal/hub/hub.go", "Run"},
	{"internal/hub/hub_edge.go", "Serve"},
	{"internal/hub/hub_edge.go", "RunSlot"},
}

// Every production osproc.ReapInherited call runs before anything that could start a child: only the listed calls
// dominate it (earlier statements of its enclosing blocks and the conditions of the ifs and switches it is in; a
// branch that always returns or exits is skipped, since it never reaches the call).
func TestReapInheritedRunsBeforeAnyChildStarts(t *testing.T) {
	fset := token.NewFileSet()
	for _, site := range reapSites {
		fn := parseFunc(t, fset, site.file, site.fn)
		var reaps []*ast.CallExpr
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && callName(c) == reapCall {
				reaps = append(reaps, c)
			}
			return true
		})
		if len(reaps) != site.reaps {
			t.Errorf("%s: %s calls %s %d times, want %d", pos(fset, fn), site.fn, reapCall, len(reaps), site.reaps)
			continue
		}
		used := map[string]bool{}
		for _, reap := range reaps {
			var calls []*ast.CallExpr
			if !callsBefore(fn.Body.List, reap, &calls) {
				t.Errorf("%s: %s is not a statement of its own in %s's blocks, ifs and switches", pos(fset, reap), reapCall, site.fn)
				continue
			}
			for _, c := range calls {
				name := callName(c)
				if _, ok := site.before[name]; !ok {
					t.Errorf("%s: %s runs before %s (%s) in %s and may start a child the reaper then takes; "+
						"move the reap above it, or list it in reapSites with why it starts no process",
						pos(fset, c), name, reapCall, pos(fset, reap), site.fn)
				}
				used[name] = true
			}
		}
		for name := range site.before {
			if !used[name] {
				t.Errorf("%s: reapSites allows %s ahead of %s in %s, which no longer calls it there: drop the entry",
					site.file, name, reapCall, site.fn)
			}
		}
	}
	for _, c := range hubClaimers {
		fn := parseFunc(t, fset, c.file, c.fn)
		var first ast.Expr
		if len(fn.Body.List) > 0 {
			if s, ok := fn.Body.List[0].(*ast.ExprStmt); ok {
				first = s.X
			}
		}
		if call, ok := first.(*ast.CallExpr); !ok || callName(call) != "claimInherited" {
			t.Errorf("%s: hub %s must call claimInherited as its first statement (listen.go)", pos(fset, fn), c.fn)
		}
	}
}

// A new production call site has to be listed above, so that its place is pinned too. Every reference counts, under
// whatever name the file imports osproc (an alias or a dot import), in a package-level func literal, or taken as a
// value; only a plain osproc.ReapInherited() call in a listed function passes, the shape the test above checks.
func TestEveryReapInheritedCallSiteIsPinned(t *testing.T) {
	pinned := map[string]bool{}
	for _, s := range reapSites {
		pinned[s.file+" "+s.fn] = true
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(serverDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "osproc" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(src, []byte("ReapInherited")) {
			return err
		}
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return err
		}
		names := osprocNames(f)
		if len(names) == 0 {
			return nil
		}
		rel := serverPath(path)
		for _, decl := range f.Decls {
			owner := "" // package level: never a listed site
			if fd, ok := decl.(*ast.FuncDecl); ok {
				owner = fd.Name.Name
			}
			called := map[ast.Expr]bool{}
			ast.Inspect(decl, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					called[c.Fun] = true
				}
				return true
			})
			for _, ref := range reapRefs(decl, names) {
				switch {
				case owner == "" || !pinned[rel+" "+owner]:
					t.Errorf("%s: %s outside a function in reapSites (reap_test.go): list the function so its place is checked",
						pos(fset, ref), reapCall)
				case exprName(ref) != reapCall || !called[ref]:
					t.Errorf("%s: write %s in %s as a plain %s() call, the only shape reapSites checks",
						pos(fset, ref), reapCall, owner, reapCall)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// osprocNames are the names f refers to the osproc package by: "osproc", an alias, "." for a dot import.
func osprocNames(f *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, imp := range f.Imports {
		if imp.Path.Value != `"github.com/nwverse/persona/server/internal/osproc"` {
			continue
		}
		if imp.Name == nil {
			names["osproc"] = true
		} else if imp.Name.Name != "_" {
			names[imp.Name.Name] = true
		}
	}
	return names
}

// reapRefs returns every expression in n that names osproc.ReapInherited, with osproc imported under names.
func reapRefs(n ast.Node, names map[string]bool) []ast.Expr {
	var refs []ast.Expr
	var visit func(ast.Node) bool
	visit = func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.SelectorExpr:
			if x, ok := e.X.(*ast.Ident); ok && names[x.Name] && e.Sel.Name == "ReapInherited" {
				refs = append(refs, e)
			}
			ast.Inspect(e.X, visit) // the selected name is a field or method, not the dot-imported function
			return false
		case *ast.Ident:
			if names["."] && e.Name == "ReapInherited" {
				refs = append(refs, e)
			}
		}
		return true
	}
	ast.Inspect(n, visit)
	return refs
}

// parseFunc parses file (under server/) and returns its one function or method named name.
func parseFunc(t *testing.T, fset *token.FileSet, file, name string) *ast.FuncDecl {
	t.Helper()
	f, err := parser.ParseFile(fset, filepath.Join(serverDir, filepath.FromSlash(file)), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found []*ast.FuncDecl
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == name && fd.Body != nil {
			found = append(found, fd)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s: %d functions named %s, want 1 (moved? update reapSites in reap_test.go)", file, len(found), name)
	}
	return found[0]
}

// callsBefore walks stmts down to the statement that is target and appends the calls that run ahead of it on the way:
// earlier statements, if conditions and switch tags and cases. It reports false when target sits anywhere else (in a
// loop, a condition, a deferred or goroutine call), where its order cannot be read this way.
func callsBefore(stmts []ast.Stmt, target *ast.CallExpr, out *[]*ast.CallExpr) bool {
	for _, s := range stmts {
		if !encloses(s, target) {
			reached(s, out)
			continue
		}
		switch s := s.(type) {
		case *ast.ExprStmt:
			return s.X == target
		case *ast.BlockStmt:
			return callsBefore(s.List, target, out)
		case *ast.IfStmt:
			calls(s.Init, out)
			calls(s.Cond, out)
			if encloses(s.Body, target) {
				return callsBefore(s.Body.List, target, out)
			}
			return s.Else != nil && callsBefore([]ast.Stmt{s.Else}, target, out)
		case *ast.SwitchStmt:
			calls(s.Init, out)
			calls(s.Tag, out)
			for _, cc := range s.Body.List {
				clause := cc.(*ast.CaseClause)
				for _, e := range clause.List {
					calls(e, out)
				}
				if encloses(clause, target) {
					return callsBefore(clause.Body, target, out)
				}
			}
		}
		return false
	}
	return false
}

// reached appends the calls statement s can make on a path that goes on past it: the branches of an if or a switch
// that always return or exit are left out.
func reached(s ast.Stmt, out *[]*ast.CallExpr) {
	switch s := s.(type) {
	case *ast.BlockStmt:
		branch(s.List, out)
	case *ast.IfStmt:
		calls(s.Init, out)
		calls(s.Cond, out)
		branch(s.Body.List, out)
		if s.Else != nil {
			branch([]ast.Stmt{s.Else}, out)
		}
	case *ast.SwitchStmt:
		calls(s.Init, out)
		calls(s.Tag, out)
		for _, cc := range s.Body.List {
			clause := cc.(*ast.CaseClause)
			for _, e := range clause.List {
				calls(e, out)
			}
			branch(clause.Body, out)
		}
	default:
		calls(s, out)
	}
}

// branch appends the calls of a branch's statements unless the branch always ends in a return, os.Exit or panic.
func branch(stmts []ast.Stmt, out *[]*ast.CallExpr) {
	if len(stmts) > 0 {
		switch last := stmts[len(stmts)-1].(type) {
		case *ast.ReturnStmt:
			return
		case *ast.ExprStmt:
			if c, ok := last.X.(*ast.CallExpr); ok && slices.Contains([]string{"os.Exit", "panic"}, callName(c)) {
				return
			}
		}
	}
	for _, s := range stmts {
		reached(s, out)
	}
}

// calls appends every call in n, those in function literals too (one may be called right there).
func calls(n ast.Node, out *[]*ast.CallExpr) {
	if n == nil {
		return
	}
	ast.Inspect(n, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			*out = append(*out, c)
		}
		return true
	})
}

func encloses(n, target ast.Node) bool { return n.Pos() <= target.Pos() && target.End() <= n.End() }

// callName is the called expression as written: procs.AtStart, osproc.SealInherited, e.ownDatabase,
// os.Stdout.WriteString.
func callName(c *ast.CallExpr) string { return exprName(c.Fun) }

func exprName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprName(e.X) + "." + e.Sel.Name
	case *ast.ParenExpr:
		return exprName(e.X)
	}
	return fmt.Sprintf("(%T)", e)
}

// pos is n's place as <file under server/>:<line>.
func pos(fset *token.FileSet, n ast.Node) string {
	p := fset.Position(n.Pos())
	return fmt.Sprintf("%s:%d", serverPath(p.Filename), p.Line)
}

// serverPath is path (under serverDir) relative to server/, with slashes.
func serverPath(path string) string {
	if rel, err := filepath.Rel(serverDir, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return path
}
