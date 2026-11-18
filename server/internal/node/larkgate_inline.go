package node

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/lazyre"
	"github.com/nwverse/persona/server/internal/proto"
	"mvdan.cc/sh/v3/syntax"
)

// Rewrites that show the gate what a command runs where its splitter gives up (#8148): a program handed to an
// interpreter in a quoted here-document becomes the same program given inline, a subshell or group of one command
// becomes that command, and a command path assigned with ~ or $HOME resolves like an absolute one. They are for
// classification only; nothing is run.

// larkInlineSyntax applies the first two rewrites to tree; true when it changed anything.
func larkInlineSyntax(tree *syntax.File, gates []proto.CommandGate) bool {
	changed := false
	syntax.Walk(tree, func(node syntax.Node) bool {
		stmt, ok := node.(*syntax.Stmt)
		if !ok {
			return true
		}
		if larkInlineGroup(stmt, gates) {
			changed = true
		}
		if larkInlineStdinProgram(stmt) {
			changed = true
		}
		return true
	})
	return changed
}

// larkInlineGroup turns `( cmd )` or `{ cmd; }` holding one statement into that statement. Only when the statement
// assigns nothing and runs nothing that changes the shell (cd, export, read…): then what runs, and what each word
// names, is the same in or out of the group. The group's own redirections and flags stay a group, and so does one
// that names lark-cli or a gate: their calls are read one by one only outside groups.
func larkInlineGroup(stmt *syntax.Stmt, gates []proto.CommandGate) bool {
	var inner []*syntax.Stmt
	switch c := stmt.Cmd.(type) {
	case *syntax.Subshell:
		inner = c.Stmts
	case *syntax.Block:
		inner = c.Stmts
	default:
		return false
	}
	if len(inner) != 1 || len(stmt.Redirs) != 0 || stmt.Background || stmt.Coprocess {
		return false
	}
	only := inner[0]
	if only.Background || only.Coprocess || !larkStatelessStmt(only) {
		return false
	}
	var text bytes.Buffer
	if syntax.NewPrinter().Print(&text, only) != nil || larkCLIRe.MatchString(text.String()) || gateNamed(gates, text.String()) {
		return false
	}
	stmt.Cmd, stmt.Redirs = only.Cmd, only.Redirs
	stmt.Negated = stmt.Negated != only.Negated
	return true
}

// larkStatelessStmt: simple commands, alone or in pipes and lists, that assign nothing and are not builtins that
// change the shell running them.
func larkStatelessStmt(stmt *syntax.Stmt) bool {
	switch c := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		if len(c.Assigns) != 0 || len(c.Args) == 0 {
			return false
		}
		words := make([]string, len(c.Args))
		for i, w := range c.Args {
			words[i], _ = executionWord(w)
		}
		rest, ok := unwrapCommand(words)
		if !ok || len(rest) == 0 || larkShellState[words[0]] || larkShellState[rest[0]] {
			return false
		}
		return rest[0] != "printf" || !slices.Contains(rest, "-v") // printf -v assigns
	case *syntax.BinaryCmd:
		return larkStatelessStmt(c.X) && larkStatelessStmt(c.Y)
	}
	return false
}

// larkShellState: builtins that change the shell they run in (its variables, directory, options, traps, functions).
var larkShellState = map[string]bool{"cd": true, "pushd": true, "popd": true, "export": true, "unset": true, "set": true,
	"shopt": true, "declare": true, "typeset": true, "local": true, "readonly": true, "read": true, "mapfile": true,
	"readarray": true, "getopts": true, "alias": true, "unalias": true, "eval": true, "source": true,
	".": true, "trap": true, "exec": true, "hash": true, "enable": true, "umask": true, "let": true, "builtin": true,
	"command": true, "exit": true, "return": true, "shift": true, "ulimit": true, "wait": true, "fg": true, "bg": true}

// larkInlineStdinProgram turns `[wrappers] python3 [flags] - [args…] <<'EOF' … EOF` (or `node -`) into
// `python3 [flags] -c '…' [args…]`: the program is all in the command either way. Only a here-document whose
// delimiter is quoted, so the shell expands nothing in it, and only when nothing else feeds the interpreter's input.
func larkInlineStdinProgram(stmt *syntax.Stmt) bool {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return false
	}
	doc := -1
	for i, r := range stmt.Redirs {
		switch r.Op {
		case syntax.Hdoc, syntax.DashHdoc:
			if doc >= 0 || r.N != nil && r.N.Value != "0" {
				return false
			}
			doc = i
		case syntax.RdrIn, syntax.RdrInOut, syntax.WordHdoc, syntax.DplIn:
			if r.N == nil || r.N.Value == "0" {
				return false
			}
		}
	}
	if doc < 0 {
		return false
	}
	body, ok := larkQuotedHeredoc(stmt.Redirs[doc])
	if !ok {
		return false
	}
	words := make([]string, len(call.Args))
	for i, w := range call.Args {
		var dynamic bool
		if words[i], dynamic = executionWord(w); dynamic {
			words[i] = "$" // a program argument may be anything; the interpreter and its flags may not
		}
	}
	rest, parsed := unwrapCommand(words)
	if !parsed || len(rest) == 0 {
		return false
	}
	at := len(words) - len(rest) // the interpreter
	base := filepath.Base(rest[0])
	flag, flags := "-c", larkPythonStdinFlags
	switch {
	case strings.HasPrefix(base, "python") && larkInterpreter.MatchString(base):
	case base == "node" || base == "nodejs":
		flag, flags = "-e", nil
	default:
		return false
	}
	i := 1
	for i < len(rest) && rest[i] != "-" && flags != nil && flags.MatchString(rest[i]) {
		i++
	}
	switch {
	case i < len(rest) && rest[i] == "-": // the program from input, then its arguments
	case i == len(rest): // no script named: the program from input
	default:
		return false // a script file, a module, inline code already, or a flag read differently
	}
	code, err := syntax.NewParser().Parse(strings.NewReader("x "+larkSingleQuote(body)), "")
	if err != nil || len(code.Stmts) != 1 {
		return false
	}
	codeCall, ok := code.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok || len(codeCall.Args) != 2 {
		return false
	}
	args := append([]*syntax.Word{}, call.Args[:at+i]...)
	args = append(args, &syntax.Word{Parts: []syntax.WordPart{&syntax.Lit{Value: flag}}}, codeCall.Args[1])
	if i < len(rest) {
		args = append(args, call.Args[at+i+1:]...)
	}
	call.Args = args
	stmt.Redirs = append(stmt.Redirs[:doc:doc], stmt.Redirs[doc+1:]...)
	return true
}

// larkPythonStdinFlags: python options before `-` that take no value and change neither where the program comes
// from nor that it ends when its code does (-i would read more input).
var larkPythonStdinFlags = lazyre.New(`^-[IEsSBuOqbdvP]+$`)

// larkQuotedHeredoc is a here-document's text when its delimiter is quoted: the shell expands nothing in it.
func larkQuotedHeredoc(r *syntax.Redirect) (string, bool) {
	if r.Word == nil || r.Hdoc == nil {
		return "", false
	}
	quoted := false
	for _, p := range r.Word.Parts {
		switch p := p.(type) {
		case *syntax.SglQuoted, *syntax.DblQuoted:
			quoted = true
		case *syntax.Lit:
			quoted = quoted || strings.Contains(p.Value, "\\")
		default:
			return "", false
		}
	}
	if !quoted {
		return "", false
	}
	var b strings.Builder
	for _, p := range r.Hdoc.Parts {
		lit, ok := p.(*syntax.Lit)
		if !ok {
			return "", false
		}
		b.WriteString(lit.Value)
	}
	return b.String(), true
}

// larkSingleQuote quotes s as one shell word that the gate's splitter reads back as s.
func larkSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// larkHomeWord is a literal path written from the shell's home (`~/…`, `$HOME/…`, `${HOME}/…`) with the home put in,
// for a command that does not set HOME itself; ok is false for anything else.
func larkHomeWord(word *syntax.Word, cmd string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || strings.Contains(cmd, "HOME=") || len(word.Parts) == 0 {
		return "", false
	}
	parts, quoted := word.Parts, false
	if q, ok := parts[0].(*syntax.DblQuoted); ok && len(parts) == 1 {
		parts, quoted = q.Parts, true // "$HOME/…": no ~ expansion in quotes
	}
	var b strings.Builder
	for i, p := range parts {
		switch p := p.(type) {
		case *syntax.Lit:
			v := p.Value
			if i == 0 && !quoted && (v == "~" || strings.HasPrefix(v, "~/")) {
				v = home + v[1:]
			}
			if strings.ContainsAny(v, "\\~") {
				return "", false
			}
			b.WriteString(v)
		case *syntax.ParamExp:
			if i != 0 || p.Param == nil || p.Param.Value != "HOME" || p.Excl || p.Length || p.Width || p.Index != nil ||
				p.Slice != nil || p.Repl != nil || p.Exp != nil || p.Names != 0 {
				return "", false
			}
			b.WriteString(home)
		default:
			return "", false
		}
	}
	v := b.String()
	if v == home || !strings.HasPrefix(v, home+"/") {
		return "", false
	}
	return v, true
}

// inertPythonCode: inline Python (python -c, or a here-document program rewritten so) that can start no program,
// run no other code and reach no module that does: every import is one of larkPythonPure, and outside its string
// literals and comments it names nothing that reaches a process, the interpreter's internals or dynamic lookup.
// Text such code holds, lark-cli and backticks included, is data (#8148).
func inertPythonCode(words []string) bool {
	if len(words) < 3 || !strings.HasPrefix(filepath.Base(words[0]), "python") || !larkInterpreter.MatchString(filepath.Base(words[0])) {
		return false
	}
	code := ""
	for i := 1; i < len(words) && code == ""; i++ {
		switch w := words[i]; {
		case w == "-c" && i+1 < len(words):
			code = words[i+1]
			for _, a := range words[i+2:] {
				if strings.ContainsAny(a, "|<>&") {
					return false // as selfContainedCode
				}
			}
		case !larkPythonStdinFlags.MatchString(w):
			return false // a program file, stdin, a module, or an option read differently
		}
	}
	bare, ok := pythonWithoutStrings(code)
	if code == "" || !ok || larkCodeRuns.MatchString(bare) || larkPythonReach.MatchString(bare) || larkCLIRe.MatchString(bare) ||
		strings.Contains(bare, "*") && regexp.MustCompile(`\bimport\s+\*`).MatchString(bare) {
		return false
	}
	// Every function it calls by name is a builtin that runs nothing, or defined or imported (from larkPythonPure) in
	// the code itself: a name from nowhere (`run([…])`) may stand for something that runs a program.
	known := map[string]bool{}
	for _, m := range larkPythonDefined.FindAllStringSubmatch(bare, -1) {
		known[m[1]+m[2]] = true
	}
	for _, m := range larkPythonFromImport.FindAllStringSubmatch(bare, -1) {
		for _, name := range strings.Split(strings.Trim(m[1], "() \t"), ",") {
			if f := strings.Fields(name); len(f) > 0 {
				known[f[len(f)-1]] = true // `x as y` names y
			}
		}
	}
	for _, m := range larkPythonCall.FindAllStringSubmatch(bare, -1) {
		if !known[m[1]] && !larkPythonBuiltins[m[1]] {
			return false
		}
	}
	for _, m := range larkPythonImport.FindAllStringSubmatch(bare, -1) {
		mods := m[2]
		if m[3] == "from" {
			mods = m[4]
		}
		for _, mod := range strings.Split(mods, ",") {
			mod = strings.TrimSpace(mod)
			if f := strings.Fields(mod); len(f) > 0 {
				mod = f[0] // `import x as y`
			}
			top, _, _ := strings.Cut(mod, ".")
			if !larkPythonPure[top] {
				return false
			}
		}
	}
	// Any other use of the word import (a bare `import`, an import inside a string we could not see) is not proof.
	return strings.Count(bare, "import") == len(larkPythonImport.FindAllString(bare, -1))
}

var (
	// larkPythonImport: `import a, b.c as d` (2) and `from a.b import c` (3).
	larkPythonImport = lazyre.New(`(?m)(?:^|;)[ \t]*(?:(import)[ \t]+([\w., \t]+)|(from)[ \t]+([\w.]+)[ \t]+import\b)`)
	// larkPythonReach: names that reach a process, other code or the interpreter's internals, as words of the code.
	larkPythonReach = lazyre.New(`\b(?:os|posix|nt|modules|subprocess|system|popen\w*|spawn\w*|exec\w*|fork\w*|eval|` +
		`compile|getattr|setattr|delattr|attrgetter|itemgetter|methodcaller|vars|globals|locals|breakpoint|help|pty|ctypes|importlib|runpy|pickle|marshal)\b|` +
		`(?:^|[^\w])_+[A-Za-z]`)
	// larkPythonCall: a call by bare name (not a method), and what code defines: `def f`, `class C`, `f = lambda`.
	larkPythonCall       = lazyre.New(`(?:^|[^.\w])([A-Za-z_]\w*)\s*\(`)
	larkPythonDefined    = lazyre.New(`(?m)(?:^|;)[ \t]*(?:(?:def|class)[ \t]+(\w+)|(\w+)[ \t]*=[ \t]*lambda\b)`)
	larkPythonFromImport = lazyre.New(`(?m)(?:^|;)[ \t]*from[ \t]+[\w.]+[ \t]+import[ \t]+(\([^)]*\)|[^;\n]+)`)
	// larkPythonBuiltins: keywords that take parentheses and builtins that start nothing, run no code and load none.
	larkPythonBuiltins = map[string]bool{"if": true, "elif": true, "while": true, "for": true, "in": true, "not": true,
		"and": true, "or": true, "return": true, "yield": true, "assert": true, "del": true, "with": true, "as": true,
		"is": true, "lambda": true, "else": true, "print": true, "open": true, "len": true, "range": true, "sorted": true,
		"reversed": true, "enumerate": true, "zip": true, "map": true, "filter": true, "min": true, "max": true,
		"sum": true, "any": true, "all": true, "abs": true, "round": true, "divmod": true, "pow": true, "str": true,
		"int": true, "float": true, "bool": true, "bytes": true, "bytearray": true, "list": true, "dict": true,
		"set": true, "frozenset": true, "tuple": true, "repr": true, "format": true, "chr": true, "ord": true,
		"hex": true, "oct": true, "bin": true, "isinstance": true, "issubclass": true, "hasattr": true, "iter": true,
		"next": true, "hash": true, "id": true, "slice": true, "type": true, "super": true, "object": true,
		"Exception": true, "ValueError": true, "KeyError": true, "TypeError": true, "RuntimeError": true,
		"SystemExit": true, "AssertionError": true, "IndexError": true, "OSError": true, "StopIteration": true}
	// larkPythonPure: standard modules that start no program and load no code by name.
	larkPythonPure = map[string]bool{"__future__": true, "json": true, "re": true, "sys": true, "time": true,
		"datetime": true, "collections": true, "itertools": true, "functools": true, "math": true, "statistics": true,
		"string": true, "textwrap": true, "unicodedata": true, "difflib": true, "pathlib": true, "csv": true,
		"hashlib": true, "base64": true, "glob": true, "fnmatch": true, "operator": true, "dataclasses": true,
		"typing": true, "enum": true, "heapq": true, "bisect": true, "copy": true, "pprint": true, "decimal": true,
		"fractions": true, "random": true, "html": true, "io": true, "struct": true, "calendar": true}
)

// pythonWithoutStrings is Python code with its string literals emptied and its comments removed; f-strings keep
// their text (their braces hold code). false when a literal does not end.
func pythonWithoutStrings(code string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(code); {
		c := code[i]
		switch {
		case c == '#':
			for i < len(code) && code[i] != '\n' {
				i++
			}
		case c == '\'' || c == '"':
			j := i - 1
			for j >= 0 && strings.IndexByte("rRbBuUfF", code[j]) >= 0 {
				j--
			}
			prefix := code[j+1 : i]
			if j >= 0 && (code[j] == '_' || code[j] >= '0' && code[j] <= '9' || code[j] >= 'a' && code[j] <= 'z' || code[j] >= 'A' && code[j] <= 'Z') {
				prefix = "" // the letters end a name, not a prefix
			}
			quote := code[i : i+1]
			if strings.HasPrefix(code[i:], strings.Repeat(quote, 3)) {
				quote = strings.Repeat(quote, 3)
			}
			end := i + len(quote)
			for {
				if end >= len(code) {
					return "", false
				}
				if code[end] == '\\' {
					end += 2
					continue
				}
				if strings.HasPrefix(code[end:], quote) {
					break
				}
				if len(quote) == 1 && code[end] == '\n' {
					return "", false
				}
				end++
			}
			if strings.ContainsAny(prefix, "fF") {
				b.WriteString(code[i : end+len(quote)])
			} else {
				b.WriteString(`""`)
			}
			i = end + len(quote)
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), true
}

// pythonWrites lists the notes inert Python code (inertPythonCode) may write: every file it opens is a note
// (larkNoteFile) named by a literal, or by a name assigned only literals. false when it may write anything else.
func pythonWrites(words []string) ([]string, bool) {
	code := ""
	for i, w := range words {
		if w == "-c" && i+1 < len(words) {
			code = words[i+1]
			break
		}
	}
	bare, ok := pythonWithoutStrings(code)
	if !ok || larkPythonFileAPI.MatchString(bare) {
		return nil, false
	}
	calls := larkPythonOpen.FindAllStringSubmatch(code, -1)
	if len(calls) != strings.Count(bare, "open") {
		return nil, false // an open( the pattern does not read, or open named otherwise
	}
	var files []string
	for _, c := range calls {
		names := []string{c[2] + c[3]}
		if c[1] != "" {
			names = nil
			for _, a := range regexp.MustCompile(`(?m)^[ \t]*`+c[1]+`[ \t]*=[ \t]*(.*)$`).FindAllStringSubmatch(code, -1) {
				v := strings.TrimSpace(a[1])
				if len(v) < 2 || v[0] != v[len(v)-1] || v[0] != '\'' && v[0] != '"' || strings.ContainsAny(v[1:len(v)-1], "'\"\\") {
					return nil, false // assigned something other than a literal
				}
				names = append(names, v[1:len(v)-1])
			}
			// Assigned in any other way (augmented, unpacked, a loop or with) is not proof.
			if len(names) == 0 || len(names) != len(regexp.MustCompile(`\b`+c[1]+`\b\s*[-+*/%|&^]?=[^=]`).FindAllString(bare, -1)) ||
				regexp.MustCompile(`\b(?:for|as)\s+[\w, ]*\b`+c[1]+`\b`).MatchString(bare) {
				return nil, false
			}
		}
		for _, n := range names {
			if !larkNoteFile.MatchString(n) {
				return nil, false
			}
			files = append(files, n)
		}
	}
	return files, true
}

var (
	// larkPythonOpen: open(NAME, …), open('literal', …) or open("literal", …).
	larkPythonOpen = lazyre.New(`(?:^|[^.\w])open\s*\(\s*(?:([A-Za-z_]\w*)|'([^'"\\\n]*)'|"([^'"\\\n]*)")\s*[,)]`)
	// larkPythonFileAPI: other ways inert code could write a file.
	larkPythonFileAPI = lazyre.New(`\b(?:pathlib|Path|io|write_text|write_bytes|touch|rename|symlink\w*|\w*link\w*|mkdir|makedirs)\b`)
)

// larkArithmetic: a substitution the splitter read as `$(…)` (nested, without the outer parentheses) that the shell
// reads as arithmetic, `$((…))`; inner are the commands of the substitutions inside it.
func larkArithmetic(nested string) (inner []string, ok bool) {
	if !strings.HasPrefix(nested, "(") || !strings.HasSuffix(nested, ")") {
		return nil, false
	}
	tree, err := syntax.NewParser().Parse(strings.NewReader("x=$("+nested+")"), "")
	if err != nil || len(tree.Stmts) != 1 {
		return nil, false
	}
	call, isCall := tree.Stmts[0].Cmd.(*syntax.CallExpr)
	if !isCall || len(call.Args) != 0 || len(call.Assigns) != 1 || call.Assigns[0].Value == nil {
		return nil, false
	}
	parts := call.Assigns[0].Value.Parts
	if len(parts) != 1 {
		return nil, false
	}
	arith, isArith := parts[0].(*syntax.ArithmExp)
	if !isArith {
		return nil, false
	}
	failed := false
	syntax.Walk(arith, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.CmdSubst:
			var out bytes.Buffer
			if syntax.NewPrinter().Print(&out, &syntax.File{Stmts: n.Stmts}) != nil {
				failed = true
			}
			inner = append(inner, out.String())
			return false
		case *syntax.ProcSubst:
			failed = true
		}
		return true
	})
	return inner, !failed
}
