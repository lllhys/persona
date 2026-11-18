package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// listenFuncs are the ways to make a listener other than osproc.Listen and osproc.FileListener, by package: on macOS a
// listener from them accepts a connection that a process started at that moment inherits and keeps open for as long as
// it lives (osproc/listen.go, #7765). A net.ListenConfig counts by its name, since its Listen method needs no other.
var listenFuncs = map[string][]string{
	"net":        {"Listen", "ListenTCP", "ListenUnix", "FileListener", "ListenConfig"},
	"net/http":   {"ListenAndServe", "ListenAndServeTLS"},
	"crypto/tls": {"Listen"},
}

// listenMethods listen by themselves on whatever value has them: an http.Server's.
var listenMethods = []string{"ListenAndServe", "ListenAndServeTLS"}

// listenTypes are net's listener types. No listener here is one, since osproc's keep net's to themselves: an assertion
// or a switch case naming one never matches, and a value of one could only come from a net function above.
var listenTypes = []string{"TCPListener", "UnixListener"}

// listenAllowed are the references that may stay, as "<file under server/> <reference>", each with why nothing it
// makes accepts a connection.
var listenAllowed = map[string]string{
	"internal/mdns/mdns.go net.ListenConfig": "multicast UDP sockets only (ListenPacket): nothing to accept",
}

// Every listener in the server module's production code comes from osproc.Listen or osproc.FileListener. Left out:
// osproc itself, which makes them, and a package's test helpers (db/dbtest, voice/voicetest), which only tests import.
func TestListenersComeFromOsproc(t *testing.T) {
	used := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(serverDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == "testdata" || name == "osproc" || name == filepath.Base(filepath.Dir(p))+"test" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel := serverPath(p)
		for _, ref := range listenerRefs(f) {
			key := rel + " " + ref.name
			if _, ok := listenAllowed[key]; ok {
				used[key] = true
				continue
			}
			if ref.typ {
				t.Errorf("%s: %s is net's listener type, which no listener here is (osproc's keep net's to themselves, "+
					"#7765): use *osproc.TCPListener or *osproc.UnixListener", pos(fset, ref.node), ref.name)
				continue
			}
			t.Errorf("%s: %s makes a listener whose connections a process started at that moment inherits on macOS: "+
				"use osproc.Listen or osproc.FileListener (#7765), or list it in listenAllowed with why nothing it "+
				"makes accepts a connection", pos(fset, ref.node), ref.name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for key := range listenAllowed {
		if !used[key] {
			t.Errorf("listenAllowed lists %q, which is no longer there: drop the entry", key)
		}
	}
}

// listenerRefs finds the references under any import name, and only those.
func TestListenerRefs(t *testing.T) {
	for _, c := range []struct {
		src  string
		want []string
	}{
		{`import "net"; var _, _ = net.Listen("tcp", ""); var _ = net.Dial`, []string{"net.Listen"}},
		{`import n "net"; var _ = n.FileListener; var _ net.ListenConfig`, []string{"net.FileListener"}},
		{`import . "net"; var _ ListenConfig; func f() { _, _ = FileListener(nil) }`, []string{"net.ListenConfig", "net.FileListener"}},
		{`import ("crypto/tls"; "net/http"); func f(s *http.Server) { _ = s.ListenAndServe(); _ = http.ListenAndServeTLS; _ = tls.Listen }`,
			[]string{".ListenAndServe", "http.ListenAndServeTLS", "tls.Listen"}},
		{`import "net"; func f(l net.Listener) { _, _ = l.(*net.UnixListener); switch l.(type) { case *net.TCPListener: } }`,
			[]string{"net.UnixListener", "net.TCPListener"}},
		{`import . "net"; func f(l Listener) { _ = l.(*TCPListener) }`, []string{"net.TCPListener"}},
		{`import "github.com/nwverse/persona/server/internal/osproc"; var _, _ = osproc.Listen("tcp", ""); var _ *osproc.UnixListener`, nil},
		{`func f(s interface{ Listen(string) error }) { _ = s.Listen("tcp") }`, nil}, // a userspace network's, say
	} {
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", "package x; "+c.src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, ref := range listenerRefs(f) {
			got = append(got, ref.name)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: found %q, want %q", c.src, got, c.want)
		}
	}
}

type listenerRef struct {
	node ast.Node
	name string // as written for the package's own name: net.Listen, http.ListenAndServe; .ListenAndServe for a method
	typ  bool   // one of listenTypes
}

// listenerRefs are f's references to listenFuncs and listenTypes, under whatever name f imports their package by (an
// alias or a dot import), and its calls of listenMethods on a value.
func listenerRefs(f *ast.File) []listenerRef {
	pkgs := map[string]string{} // the name f uses for a package of listenFuncs ("." for a dot import) -> its path
	var dotted []string
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || listenFuncs[p] == nil {
			continue
		}
		switch {
		case imp.Name == nil:
			pkgs[path.Base(p)] = p
		case imp.Name.Name == ".":
			dotted = append(dotted, p)
		case imp.Name.Name != "_":
			pkgs[imp.Name.Name] = p
		}
	}
	var refs []listenerRef
	add := func(n ast.Node, p, name string) {
		typ := p == "net" && slices.Contains(listenTypes, name)
		if typ || slices.Contains(listenFuncs[p], name) {
			refs = append(refs, listenerRef{n, path.Base(p) + "." + name, typ})
		}
	}
	var visit func(ast.Node) bool
	visit = func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.SelectorExpr:
			if x, ok := e.X.(*ast.Ident); ok && pkgs[x.Name] != "" {
				add(e, pkgs[x.Name], e.Sel.Name)
				return false
			}
			if slices.Contains(listenMethods, e.Sel.Name) {
				refs = append(refs, listenerRef{e, "." + e.Sel.Name, false})
			}
			ast.Inspect(e.X, visit) // the selected name is a field or method, not a dot-imported function
			return false
		case *ast.Ident:
			for _, p := range dotted {
				add(e, p, e.Name)
			}
		}
		return true
	}
	for _, decl := range f.Decls {
		ast.Inspect(decl, visit)
	}
	return refs
}
