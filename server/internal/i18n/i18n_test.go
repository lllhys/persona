package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	for tag, want := range map[string]Lang{
		"zh": ZhHans, "zh-CN": ZhHans, "zh-Hans": ZhHans, "zh-Hant": ZhHans, "zh_TW": ZhHans, "ZH-hk": ZhHans,
		"ja": Ja, "ja-JP": Ja,
		"en": En, "en-GB": En, "fr": En, "ko-KR": En,
		"": "",
	} {
		if got := Resolve(tag); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", tag, got, want)
		}
	}
}

// Match is Resolve without its fallback: a tag no catalog matches is "", like an empty one.
func TestMatch(t *testing.T) {
	for tag, want := range map[string]Lang{
		"zh-CN": ZhHans, "zh_TW": ZhHans, " ja-JP ": Ja, "EN-us": En, "en": En,
		"ko-KR": "", "fr": "", "yue-CN": "", "-": "", "": "",
	} {
		if got := Match(tag); got != want {
			t.Errorf("Match(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestAcceptLanguage(t *testing.T) {
	for h, want := range map[string]Lang{
		"ja":                         Ja,
		"en-US,en;q=0.9,zh-CN;q=0.8": En,
		"zh-TW,zh;q=0.9":             ZhHans,
		"":                           "",
		"*":                          "",
		" ja-JP ; q=1, en":           Ja,
	} {
		if got := FromAcceptLanguage(h); got != want {
			t.Errorf("FromAcceptLanguage(%q) = %q, want %q", h, got, want)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "en")
	if FromRequest(r) != En {
		t.Fatal("request header not read")
	}
}

func TestT(t *testing.T) {
	if got := T(En, "push.done_body"); got != "Done" {
		t.Fatalf("en = %q", got)
	}
	if got := T("", "push.done_body"); got != "已完成" {
		t.Fatalf("unknown language should be the default, got %q", got)
	}
	if got := T(En, "push.agent_asks", Args{"agent": "Codex"}); got != "Codex has a question" {
		t.Fatalf("placeholder: %q", got)
	}
	if got := T(Ja, "no.such_key"); got != "no.such_key" {
		t.Fatalf("missing key: %q", got)
	}
}

// The three languages have the same namespaces, keys (plural suffixes aside) and placeholders.
func TestCatalogsAgree(t *testing.T) {
	plural := regexp.MustCompile(`_(zero|one|two|few|many|other)$`)
	ph := regexp.MustCompile(`\{(\w+)\}`)
	shape := func(l Lang) map[string][]string {
		out := map[string][]string{}
		for k, v := range catalogs()[l] {
			base := plural.ReplaceAllString(k, "")
			for _, m := range ph.FindAllStringSubmatch(v, -1) {
				if !slices.Contains(out[base], m[1]) {
					out[base] = append(out[base], m[1])
				}
			}
			if _, ok := out[base]; !ok {
				out[base] = []string{}
			}
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s %s is empty", l, k)
			}
		}
		for k := range out {
			slices.Sort(out[k])
		}
		return out
	}
	src := shape(ZhHans)
	for _, l := range Langs[1:] {
		got := shape(l)
		for k, ps := range src {
			g, ok := got[k]
			if !ok {
				t.Errorf("%s: missing %s", l, k)
				continue
			}
			if !slices.Equal(g, ps) {
				t.Errorf("%s %s placeholders %v, zh-Hans has %v", l, k, g, ps)
			}
		}
		for k := range got {
			if _, ok := src[k]; !ok {
				t.Errorf("%s: %s is not in zh-Hans", l, k)
			}
		}
	}
	names := func(l Lang) []string {
		var out []string
		entries, _ := fs.ReadDir(files, "locales/"+string(l))
		for _, e := range entries {
			if e.Name() == "_meta.json" || !strings.HasSuffix(e.Name(), ".json") {
				continue // Language metadata is not a message namespace.
			}
			out = append(out, e.Name())
		}
		return out
	}
	for _, l := range Langs[1:] {
		if !slices.Equal(names(l), names(ZhHans)) {
			t.Errorf("%s namespaces %v, zh-Hans has %v", l, names(l), names(ZhHans))
		}
	}
	for k, v := range catalogs()[ZhHans] {
		if strings.Contains(v, "Persona") {
			for _, l := range Langs[1:] {
				if s, ok := catalogs()[l][k]; ok && !strings.Contains(s, "Persona") {
					t.Errorf("%s %s drops Persona: %q", l, k, s)
				}
			}
		}
	}
}

func TestLinkPeerCatalogIsExhaustivelyPinned(t *testing.T) {
	var got []string
	for key := range catalogs()[ZhHans] {
		if strings.HasPrefix(key, "link-peer.") {
			got = append(got, key)
		}
	}
	slices.Sort(got)
	want := []string{
		"link-peer.bad_code",
		"link-peer.code_required",
		"link-peer.machine_required",
		"link-peer.pair_json_required",
		"link-peer.pair_save_failed",
		"link-peer.rate_limited",
		"link-peer.token_invalid",
		"link-peer.websocket_required",
		"link-peer.wrong_credential",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("link-peer keys = %v, want exactly %v; pin every new peer text in internal/link", got, want)
	}
}

// Server code's literal keys exist and get every placeholder: a missing key reads as the key itself and a missing
// argument as "{name}" (the web has the same guard in web/src/i18n/catalog.test.ts). Keys and Args built at run
// time are not checked.
func TestCallSitesUseKnownKeys(t *testing.T) {
	cat := catalogs()[ZhHans]
	ph := regexp.MustCompile(`\{(\w+)\}`)
	plurals := []string{"zero", "one", "two", "few", "many", "other"}
	root := filepath.Join("..", "..")
	calls := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		pkg := importName(f)
		if pkg == "" {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); !ok || id.Name != pkg {
				return true
			}
			at := map[string]int{"T": 1, "Err": 0, "ErrEnglish": 0}
			i, ok := at[sel.Sel.Name]
			if !ok || len(call.Args) <= i {
				return true
			}
			key, ok := stringLit(call.Args[i])
			if !ok {
				return true
			}
			calls++
			pos := fset.Position(call.Pos())
			// Exact first: a key like "pick_one" is a key, not a plural form.
			var forms []string
			_, exact := cat[key]
			if exact {
				forms = append(forms, key)
			}
			plural := false
			for _, s := range plurals {
				if _, ok := cat[key+"_"+s]; ok {
					forms = append(forms, key+"_"+s)
					plural = true
				}
			}
			if len(forms) == 0 {
				t.Errorf("%s: %s.%s key %q is not in the catalogs", pos, pkg, sel.Sel.Name, key)
				return true
			}
			if len(call.Args) <= i+1 {
				// No Args fills nothing, which some texts mean ({id} in a route); a plural-only key needs a count.
				if !exact {
					t.Errorf("%s: %s.%s(%q) has only plural forms but no count", pos, pkg, sel.Sel.Name, key)
				}
				return true
			}
			args, ok := argsLit(call.Args[i+1], pkg)
			if !ok {
				return true
			}
			var need []string
			for _, k := range forms {
				for _, m := range ph.FindAllStringSubmatch(cat[k], -1) {
					if !slices.Contains(need, m[1]) {
						need = append(need, m[1])
					}
				}
			}
			if plural && !exact && !slices.Contains(need, "count") {
				need = append(need, "count")
			}
			for _, name := range need {
				if !args[name] {
					t.Errorf("%s: %s.%s(%q) does not pass {%s}", pos, pkg, sel.Sel.Name, key, name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls < 100 {
		t.Fatalf("found only %d calls with a literal key under %s; is the walk looking in the right place?", calls, root)
	}
}

// Every catalog key is reached from server code, so a rewrite that drops a key's last caller also drops the key
// (TestCallSitesUseKnownKeys is the other direction). A key counts as used when a string literal is the key or its
// plural base, when it starts with the literal left side of a concatenation (string constants resolved, ns+"x"+n)
// or of a Sprintf format, when a wrapper like workT(l, "x") passes a literal to i18n.T(l, "prefix."+name), or when
// one of this package's catalog template filters (makeCatalogTemplates) names its prefix.
func TestEveryKeyIsUsed(t *testing.T) {
	root := filepath.Join("..", "..")
	type file struct {
		f   *ast.File
		pkg string // name this file uses for package i18n; "" when it does not import it
	}
	dirs := map[string][]file{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		dirs[filepath.Dir(p)] = append(dirs[filepath.Dir(p)], file{f, importName(f)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	exact, prefixes := map[string]bool{}, map[string]bool{}
	addPrefix := func(s string) {
		if strings.Contains(s, ".") { // a namespace at least, so a short literal cannot cover a whole catalog
			prefixes[s] = true
		}
	}
	for _, files := range dirs {
		// String constants of the package and of its functions, by name; a name bound to two values is dropped.
		consts := map[string]string{}
		for _, fl := range files {
			ast.Inspect(fl.f, func(n ast.Node) bool {
				if g, ok := n.(*ast.GenDecl); ok && g.Tok == token.CONST {
					for _, spec := range g.Specs {
						vs := spec.(*ast.ValueSpec)
						for i, name := range vs.Names {
							if i >= len(vs.Values) {
								continue
							}
							if s, ok := stringLit(vs.Values[i]); ok {
								if old, seen := consts[name.Name]; seen && old != s {
									s = "\x00"
								}
								consts[name.Name] = s
							}
						}
					}
				}
				return true
			})
		}
		// eval is e's constant string prefix; whole reports that all of e is constant.
		var eval func(ast.Expr) (s string, whole bool)
		eval = func(e ast.Expr) (string, bool) {
			switch e := e.(type) {
			case *ast.BasicLit:
				return stringLit(e)
			case *ast.Ident:
				s, ok := consts[e.Name]
				return s, ok && s != "\x00"
			case *ast.ParenExpr:
				return eval(e.X)
			case *ast.BinaryExpr:
				if e.Op != token.ADD {
					return "", false
				}
				x, whole := eval(e.X)
				if !whole {
					return x, false
				}
				y, whole := eval(e.Y)
				return x + y, whole
			}
			return "", false
		}
		// Wrappers: func f(..., name string, ...) whose body calls i18n.T(l, "prefix."+name, ...).
		type wrapper struct {
			prefix string
			arg    int
		}
		wrappers := map[string]wrapper{}
		for _, fl := range files {
			if fl.pkg == "" {
				continue
			}
			for _, decl := range fl.f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || fn.Body == nil {
					continue
				}
				params := map[string]int{}
				for _, field := range fn.Type.Params.List {
					for _, name := range field.Names {
						params[name.Name] = len(params)
					}
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok || len(call.Args) < 2 {
						return true
					}
					if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "T" || !isIdent(sel.X, fl.pkg) {
						return true
					}
					bin, ok := call.Args[1].(*ast.BinaryExpr)
					if !ok || bin.Op != token.ADD {
						return true
					}
					id, ok := bin.Y.(*ast.Ident)
					if !ok {
						return true
					}
					if i, ok := params[id.Name]; ok {
						if p, whole := eval(bin.X); whole {
							wrappers[fn.Name.Name] = wrapper{p, i}
						}
					}
					return true
				})
			}
		}
		for _, fl := range files {
			ast.Inspect(fl.f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.BasicLit:
					if s, ok := stringLit(n); ok {
						exact[s] = true
					}
				case *ast.BinaryExpr:
					if s, whole := eval(n); whole {
						exact[s] = true
					} else {
						addPrefix(s)
					}
				case *ast.CallExpr:
					switch fun := n.Fun.(type) {
					case *ast.Ident:
						if w, ok := wrappers[fun.Name]; ok && w.arg < len(n.Args) {
							if s, whole := eval(n.Args[w.arg]); whole {
								exact[w.prefix+s] = true
							}
						}
						// The catalog template filters of this package translate every key they accept.
						if fun.Name == "makeCatalogTemplates" && fl.f.Name.Name == "i18n" {
							ast.Inspect(n, func(m ast.Node) bool {
								if lit, ok := m.(*ast.BasicLit); ok {
									if s, ok := stringLit(lit); ok {
										addPrefix(s)
									}
								}
								return true
							})
						}
					case *ast.SelectorExpr:
						if isIdent(fun.X, "fmt") && fun.Sel.Name == "Sprintf" && len(n.Args) > 0 {
							if s, _ := eval(n.Args[0]); strings.Contains(s, "%") {
								addPrefix(s[:strings.Index(s, "%")])
							}
						}
					}
				}
				return true
			})
		}
	}
	plural := regexp.MustCompile(`_(zero|one|two|few|many|other)$`)
	starts := slices.Collect(maps.Keys(prefixes))
	var unused []string
	for key := range catalogs()[ZhHans] {
		if exact[key] || exact[plural.ReplaceAllString(key, "")] {
			continue
		}
		if !slices.ContainsFunc(starts, func(p string) bool { return strings.HasPrefix(key, p) }) {
			unused = append(unused, key)
		}
	}
	slices.Sort(unused)
	for _, key := range unused {
		t.Errorf("%s is in the catalogs but no server code uses it; remove it from every language", key)
	}
	if len(exact) < 1000 {
		t.Fatalf("found only %d string literals under %s; is the walk looking in the right place?", len(exact), root)
	}
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// importName is the name f uses for this package, "" when f does not import it.
func importName(f *ast.File) string {
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if !strings.HasSuffix(p, "/internal/i18n") {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "i18n"
	}
	return ""
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// argsLit is the names an <pkg>.Args{...} literal sets; false when e is something else or a name is not a literal.
func argsLit(e ast.Expr, pkg string) (map[string]bool, bool) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Args" {
		return nil, false
	}
	if id, ok := sel.X.(*ast.Ident); !ok || id.Name != pkg {
		return nil, false
	}
	out := map[string]bool{}
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			return nil, false
		}
		name, ok := stringLit(kv.Key)
		if !ok {
			return nil, false
		}
		out[name] = true
	}
	return out, true
}

// Every directory under locales is a language (docs/protocol/i18n.md "Adding a language"): it has a _meta.json with its own name and
// the language subtags it is picked for, no subtag is claimed twice, and the source and fallback languages exist.
func TestLanguagesAreTheDirectories(t *testing.T) {
	entries, err := fs.ReadDir(files, "locales")
	if err != nil {
		t.Fatal(err)
	}
	var dirs []Lang
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, Lang(e.Name()))
		}
	}
	got := slices.Clone(Langs)
	slices.Sort(got)
	slices.Sort(dirs)
	if !slices.Equal(got, dirs) {
		t.Fatalf("Langs %v, directories %v", got, dirs)
	}
	if Langs[0] != Default || Langs[1] != Fallback {
		t.Errorf("Langs %v should start with %s, %s", Langs, Default, Fallback)
	}
	claimed := map[string]Lang{}
	for _, l := range Langs {
		m := metas[l]
		if strings.TrimSpace(m.Name) == "" || len(m.Matches) == 0 {
			t.Errorf("%s/_meta.json needs a name and matches: %+v", l, m)
		}
		own, _, _ := strings.Cut(strings.ToLower(string(l)), "-")
		if !slices.Contains(m.Matches, own) {
			t.Errorf("%s/_meta.json matches %v, not its own subtag %q", l, m.Matches, own)
		}
		for _, sub := range m.Matches {
			if prev, ok := claimed[sub]; ok {
				t.Errorf("%q is matched by both %s and %s", sub, prev, l)
			}
			claimed[sub] = l
		}
	}
	if Ja.Name() != "日本語" || En.Name() != "English" || Lang("xx").Name() != ZhHans.Name() {
		t.Errorf("names: %q %q %q", Ja.Name(), En.Name(), Lang("xx").Name())
	}
}

func TestPluralForms(t *testing.T) {
	for _, c := range []struct {
		l    Lang
		n    int64
		want string
	}{{En, 1, "one"}, {En, 2, "other"}, {En, 0, "other"}, {Ja, 1, "other"}, {ZhHans, 1, "other"}, {"fr", 0, "one"}, {"ru", 3, "few"}} {
		if got := pluralForm(c.l, c.n); got != c.want {
			t.Errorf("pluralForm(%s, %d) = %s, want %s", c.l, c.n, got, c.want)
		}
	}
}
