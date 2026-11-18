package backup

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every message a test in this package prints goes through redactTestOutput:
// the real-PostgreSQL tests carry database URLs with passwords and TLS key
// passphrases, and a failure must not put them (or any other credential that
// reaches an error) in a CI log. TestPackageTestsPrintOnlyThroughTheFilter
// keeps the tests from calling t.Fatal and friends directly.
var testOutputSecrets = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)[A-Z0-9_]*(KEY|TOKEN|SECRET|PASSWORD|PASSPHRASE|CREDENTIAL)[A-Z0-9_]*=\S+`), "[REDACTED]"},
	{regexp.MustCompile(`gh[opusr]_[A-Za-z0-9]{20,}`), "[REDACTED]"},
	{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`), "[REDACTED]"},
	{regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`), "[REDACTED]"},
	{regexp.MustCompile(`Bearer \S+`), "[REDACTED]"},
	// URL userinfo (scheme://user[:password]@host): the scheme stays.
	// Up to the authority's last @, so an unencoded @ in a password is covered.
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/?#\s]*@`), "${1}[REDACTED]@"},
	// A struct printed with %+v: {Password:hunter2 ...}.
	{regexp.MustCompile(`(?i)\b[A-Z0-9_]*(KEY|TOKEN|SECRET|PASSWORD|PASSPHRASE|CREDENTIAL)[A-Z0-9_]*:[^\s,}\]]+`), "[REDACTED]"},
	// A JSON field such as "token":"..." or "password": "...".
	{regexp.MustCompile(`(?i)"[A-Z0-9_]*(KEY|TOKEN|SECRET|PASSWORD|PASSPHRASE|CREDENTIAL)[A-Z0-9_]*"\s*:\s*"[^"]*"`), "[REDACTED]"},
}

func redactTestOutput(text string) string {
	for _, secret := range testOutputSecrets {
		text = secret.pattern.ReplaceAllString(text, secret.replacement)
	}
	return text
}

// testEnvironmentAllowList names the only environment variables a failure
// message may show; their values still pass through redactTestOutput (so
// PGPASSWORD=... and PGSSLPASSWORD=... never appear).
var testEnvironmentAllowList = []string{"PG", "GOFLAGS", "GOTOOLCHAIN", "GOOS", "GOARCH", "TMPDIR", "HOME", "PATH", "LANG", "LC_ALL", "PERSONA_TEST_POSTGRES_CONTAINER"}

func environmentAllowed(name string) bool {
	for _, allowed := range testEnvironmentAllowList {
		if name == allowed || (allowed == "PG" && strings.HasPrefix(name, "PG")) {
			return true
		}
	}
	return false
}

// shownEnvironment is the form of an environment (NAME=value list or map)
// a failure message may print: allow-listed names only, values redacted.
func shownEnvironment(environment any) string {
	var variables []string
	switch environment := environment.(type) {
	case []string:
		variables = append(variables, environment...)
	case map[string]string:
		for name, value := range environment {
			variables = append(variables, name+"="+value)
		}
	default:
		return "[environment of unexpected type not shown]"
	}
	var shown []string
	hidden := 0
	for _, variable := range variables {
		name, _, _ := strings.Cut(variable, "=")
		if !environmentAllowed(name) {
			hidden++
			continue
		}
		shown = append(shown, redactTestOutput(variable))
	}
	sort.Strings(shown)
	if hidden > 0 {
		shown = append(shown, fmt.Sprintf("(%d variable(s) outside the allow-list not shown)", hidden))
	}
	return "[" + strings.Join(shown, " ") + "]"
}

func sprintTest(args ...any) string {
	return redactTestOutput(strings.TrimSuffix(fmt.Sprintln(args...), "\n"))
}

func fatal(t testing.TB, args ...any) {
	t.Helper()
	t.Fatal(sprintTest(args...))
}

func fatalf(t testing.TB, format string, args ...any) {
	t.Helper()
	t.Fatal(redactTestOutput(fmt.Sprintf(format, args...)))
}

func errorT(t testing.TB, args ...any) {
	t.Helper()
	t.Error(sprintTest(args...))
}

func errorf(t testing.TB, format string, args ...any) {
	t.Helper()
	t.Error(redactTestOutput(fmt.Sprintf(format, args...)))
}

func logf(t testing.TB, format string, args ...any) {
	t.Helper()
	t.Log(redactTestOutput(fmt.Sprintf(format, args...)))
}

func skip(t testing.TB, args ...any) {
	t.Helper()
	t.Skip(sprintTest(args...))
}

func skipf(t testing.TB, format string, args ...any) {
	t.Helper()
	t.Skip(redactTestOutput(fmt.Sprintf(format, args...)))
}

func TestTestOutputFilterRedactsCredentials(t *testing.T) {
	token := strings.Repeat("A1", 15)
	for _, sample := range []struct{ text, secret string }{
		{"SUPER_RELAY_KEY=relay-value-1", "relay-value-1"},
		{"api_token=abc123", "abc123"},
		{"PGPASSWORD=hunter2", "hunter2"},
		{"PGSSLPASSWORD=hunter2", "hunter2"},
		{"sslpassword=hunter2", "hunter2"},
		{"x?password=hunter2&sslmode=disable", "hunter2"},
		{"CLIENT_SECRET=s3cr3t", "s3cr3t"},
		{"backup_passphrase=correct-horse", "correct-horse"},
		{"AWS_CREDENTIALS=abc", "abc"},
		{"ghp_" + token, token}, {"gho_" + token, token}, {"ghu_" + token, token}, {"ghs_" + token, token}, {"ghr_" + token, token},
		{"github_pat_" + strings.Repeat("F6_", 10), strings.Repeat("F6_", 10)},
		{"sk-" + strings.Repeat("g7-", 8), strings.Repeat("g7-", 8)},
		{"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload", "eyJhbGciOiJIUzI1NiJ9"},
		{"postgres://persona:hunter2@db.example:5432/persona", "hunter2"},
		{"postgres://only-user-name@db.example/persona", "only-user-name"},
		{"https://x-access-token:hunter2@github.com/nwverse/persona.git", "hunter2"},
		{`{"database_url":"postgres://u@h/d","token":"hunter2"}`, "hunter2"},
		{`map[string]string{"PGPASSWORD":"hunter2"}`, "hunter2"},
		{"postgres://persona:hun@ter2@db.example/persona", "ter2"},
		{"{User:persona Password:hunter2 Host:db}", "hunter2"},
		{"&{SSLPassword:hunter2}", "hunter2"},
	} {
		got := redactTestOutput("failure: " + sample.text + " (end)")
		// Never print the sample itself: that is what the filter is for.
		if strings.Contains(got, sample.secret) || !strings.Contains(got, "[REDACTED]") {
			t.Errorf("a %d-byte sample was not redacted", len(sample.text))
		}
		if !strings.HasPrefix(got, "failure: ") || !strings.HasSuffix(got, " (end)") {
			t.Errorf("the filter removed more than the secret: %d bytes left", len(got))
		}
	}
	for _, kept := range []string{
		"PGHOST=db.example PGPORT=5432 PGDATABASE=persona",
		"postgres://db.example:5432/persona?sslmode=verify-full",
		"the hub and built-in manager must have separate databases",
		"secret.key is missing",
		"read secret.key: no such file",
		"set the env variable PGHOST",
	} {
		if got := redactTestOutput(kept); got != kept {
			t.Errorf("redactTestOutput(%q) = %q, want it unchanged", kept, got)
		}
	}
	// The scheme and host of a URL with userinfo stay readable.
	if got := redactTestOutput("postgres://persona:hunter2@db.example/persona"); got != "postgres://[REDACTED]@db.example/persona" {
		t.Errorf("URL userinfo is not replaced by [REDACTED] with scheme and host kept (%d bytes)", len(got))
	}
}

// recordingTB records what the helpers hand to testing.TB.
type recordingTB struct {
	testing.TB
	messages []string
}

func (r *recordingTB) Helper()           {}
func (r *recordingTB) Fatal(args ...any) { r.messages = append(r.messages, fmt.Sprint(args...)) }
func (r *recordingTB) Error(args ...any) { r.messages = append(r.messages, fmt.Sprint(args...)) }
func (r *recordingTB) Log(args ...any)   { r.messages = append(r.messages, fmt.Sprint(args...)) }
func (r *recordingTB) Skip(args ...any)  { r.messages = append(r.messages, fmt.Sprint(args...)) }
func (r *recordingTB) Fatalf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}
func (r *recordingTB) Errorf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}
func (r *recordingTB) Logf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}
func (r *recordingTB) Skipf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func TestOutputHelpersFilterEveryMessage(t *testing.T) {
	url := "postgres://persona:hunter2@db.example/persona?sslpassword=hunter2"
	recorder := &recordingTB{TB: t}
	fatal(recorder, "connect", url)
	fatalf(recorder, "connect %s", url)
	errorT(recorder, "connect", url)
	errorf(recorder, "connect %s", url)
	logf(recorder, "connect %s", url)
	skip(recorder, "connect", url)
	skipf(recorder, "connect %s", url)
	if len(recorder.messages) != 7 {
		t.Fatalf("recorded %d messages, want 7", len(recorder.messages))
	}
	for i, message := range recorder.messages {
		if strings.Contains(message, "hunter2") || !strings.HasPrefix(message, "connect ") {
			t.Errorf("helper %d passed an unfiltered message (%d bytes)", i, len(message))
		}
	}
}

func TestShownEnvironmentIsAllowListed(t *testing.T) {
	shown := shownEnvironment([]string{
		"PGHOST=db.example", "PGPASSWORD=hunter2", "GOFLAGS=-mod=mod", "TMPDIR=/tmp/x",
		"SUPER_RELAY_KEY=hunter2", "GH_TOKEN=hunter2", "OPENAI_API_KEY=hunter2", "AWS_REGION=x", "PERSONA_TEST_DATABASE_URL=postgres://u:hunter2@h/d",
	})
	if strings.Contains(shown, "hunter2") || strings.Contains(shown, "SUPER_RELAY") || strings.Contains(shown, "GH_TOKEN") ||
		strings.Contains(shown, "AWS_REGION") || strings.Contains(shown, "PERSONA_TEST_DATABASE_URL") {
		t.Errorf("environment outside the allow-list or a secret value is shown: %d bytes", len(shown))
	}
	for _, want := range []string{"PGHOST=db.example", "GOFLAGS=-mod=mod", "TMPDIR=/tmp/x", "(5 variable(s) outside the allow-list not shown)"} {
		if !strings.Contains(shown, want) {
			t.Errorf("shownEnvironment lacks %q: %s", want, shown)
		}
	}
	if shown := shownEnvironment(map[string]string{"PGUSER": "persona", "PGPASSWORD": "hunter2"}); strings.Contains(shown, "hunter2") || !strings.Contains(shown, "PGUSER=persona") {
		t.Errorf("map environment: %d bytes, PGUSER shown %v", len(shown), strings.Contains(shown, "PGUSER=persona"))
	}
}

// Every message a test here prints goes through the filter: no direct
// t.Fatal/t.Error/t.Log/t.Skip call outside this file (also through another
// name for t, such as tt := t), no fmt.Print, log.Print/Fatal/Panic,
// fmt.Fprint to os.Stdout or os.Stderr, or print/println builtin, and no test
// reads or prints the whole environment (os.Environ, syscall.Environ, or an
// env/printenv command in a fake client script). It is a lint, not a proof:
// os.Stdout.Write, a writer kept in a variable, or a call through a helper in
// another package would pass it.
func TestPackageTestsPrintOnlyThroughTheFilter(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("list the package tests: %v", err)
	}
	for _, name := range files {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, problem := range unfilteredOutput(name, source) {
			t.Error(problem)
		}
	}
}

// envCommand matches env, printenv, export -p or declare -p where a shell
// would run it as a command (at the start of the string or a line, or after
// ; | & ( { or a backquote), not the word env in prose.
var envCommand = regexp.MustCompile(`(^"|^` + "`" + `|\\n|\n|[;|&({` + "`" + `]|\$\()\s*(env|printenv|export -p|declare -p)(\s|$|[|;)"` + "`" + `]|\\n)`)

// unfilteredOutput lists the calls in one test file that print without the
// output filter.
func unfilteredOutput(name string, source []byte) []string {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, name, source, parser.SkipObjectResolution)
	if err != nil {
		return []string{fmt.Sprintf("parse %s: %v", name, err)}
	}
	printing := map[string]bool{"Fatal": true, "Fatalf": true, "Error": true, "Errorf": true, "Log": true, "Logf": true, "Skip": true, "Skipf": true}
	testingNames := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		var params *ast.FieldList
		switch node := node.(type) {
		case *ast.FuncDecl:
			params = node.Type.Params
		case *ast.FuncLit:
			params = node.Type.Params
		}
		if params != nil {
			for _, field := range params.List {
				switch types.ExprString(field.Type) {
				case "*testing.T", "testing.TB", "*testing.B", "*testing.F":
					for _, ident := range field.Names {
						testingNames[ident.Name] = true
					}
				}
			}
		}
		return true
	})
	// Another name for a testing value (tt := t, var tb testing.TB = t).
	for changed := true; changed; {
		changed = false
		ast.Inspect(file, func(node ast.Node) bool {
			var names []*ast.Ident
			var values []ast.Expr
			switch node := node.(type) {
			case *ast.AssignStmt:
				for _, lhs := range node.Lhs {
					ident, _ := lhs.(*ast.Ident)
					names = append(names, ident)
				}
				values = node.Rhs
			case *ast.ValueSpec:
				names, values = node.Names, node.Values
			}
			for i, value := range values {
				if ident, ok := value.(*ast.Ident); ok && testingNames[ident.Name] && i < len(names) && names[i] != nil && !testingNames[names[i].Name] {
					testingNames[names[i].Name] = true
					changed = true
				}
			}
			return true
		})
	}
	var problems []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			position := fileSet.Position(node.Pos())
			if ident, ok := node.Fun.(*ast.Ident); ok && (ident.Name == "print" || ident.Name == "println") {
				problems = append(problems, fmt.Sprintf("%s prints with the %s builtin, bypassing the output filter", position, ident.Name))
			}
			if selector, ok := node.Fun.(*ast.SelectorExpr); ok && len(node.Args) > 0 {
				if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "fmt" && strings.HasPrefix(selector.Sel.Name, "Fprint") {
					if stream := types.ExprString(node.Args[0]); stream == "os.Stdout" || stream == "os.Stderr" {
						problems = append(problems, fmt.Sprintf("%s prints to %s with fmt.%s, bypassing the output filter", position, stream, selector.Sel.Name))
					}
				}
			}
		case *ast.SelectorExpr:
			receiver, ok := node.X.(*ast.Ident)
			if !ok {
				return true
			}
			position := fileSet.Position(node.Pos())
			switch {
			case (receiver.Name == "os" || receiver.Name == "syscall") && node.Sel.Name == "Environ":
				problems = append(problems, fmt.Sprintf("%s reads the whole environment (%s.Environ); print only allow-listed variables", position, receiver.Name))
			case receiver.Name == "fmt" && strings.HasPrefix(node.Sel.Name, "Print"):
				problems = append(problems, fmt.Sprintf("%s prints with fmt.%s, bypassing the output filter", position, node.Sel.Name))
			case receiver.Name == "log" && (strings.HasPrefix(node.Sel.Name, "Print") || strings.HasPrefix(node.Sel.Name, "Fatal") || strings.HasPrefix(node.Sel.Name, "Panic")):
				problems = append(problems, fmt.Sprintf("%s prints with log.%s, bypassing the output filter", position, node.Sel.Name))
			case filepath.Base(name) != "testoutput_test.go" && testingNames[receiver.Name] && printing[node.Sel.Name]:
				problems = append(problems, fmt.Sprintf("%s calls %s.%s directly; use %s so the output is filtered", position, receiver.Name, node.Sel.Name, strings.ToLower(node.Sel.Name[:1])+node.Sel.Name[1:]))
			}
		case *ast.BasicLit:
			if filepath.Base(name) != "testoutput_test.go" && node.Kind == token.STRING && envCommand.MatchString(node.Value) {
				problems = append(problems, fmt.Sprintf("%s runs a command that prints the environment", fileSet.Position(node.Pos())))
			}
		}
		return true
	})
	return problems
}

// The guard catches each way around it it knows of, and not the word env.
func TestOutputGuardCatchesBypasses(t *testing.T) {
	for _, sample := range []struct{ body, want string }{
		{`t.Fatalf("x")`, "calls t.Fatalf directly"},
		{`tt := t; tt.Errorf("x")`, "calls tt.Errorf directly"},
		{`var tb testing.TB = t; tb.Log("x")`, "calls tb.Log directly"},
		{`log.Printf("x")`, "log.Printf"},
		{`log.Fatal("x")`, "log.Fatal"},
		{`fmt.Fprintln(os.Stdout, "x")`, "os.Stdout"},
		{`fmt.Fprintf(os.Stderr, "x")`, "os.Stderr"},
		{`println("x")`, "println builtin"},
		{`fmt.Println("x")`, "fmt.Println"},
		{`_ = os.Environ()`, "whole environment"},
		{`_ = "env | grep PG"`, "prints the environment"},
		{`_ = "#!/bin/sh\nprintenv\n"`, "prints the environment"},
		{"_ = `echo; env`", "prints the environment"},
		{`_ = "$(env)"`, "prints the environment"},
	} {
		source := "package backup\nimport (\"fmt\"; \"log\"; \"os\"; \"testing\")\nvar _, _, _ = fmt.Sprint, log.Print, os.Exit\nfunc TestX(t *testing.T) {\n" + sample.body + "\n}\n"
		problems := strings.Join(unfilteredOutput("sample_test.go", []byte(source)), "\n")
		if !strings.Contains(problems, sample.want) {
			t.Errorf("the guard missed %s: %q", sample.body, problems)
		}
	}
	fuzz := "package backup\nimport \"testing\"\nfunc FuzzX(f *testing.F) { f.Skip(\"x\") }\n"
	if problems := unfilteredOutput("sample_test.go", []byte(fuzz)); len(problems) != 1 {
		t.Errorf("the guard missed f.Skip on a *testing.F: %q", problems)
	}
	for _, body := range []string{
		`_ = "set the env variable PGHOST"`,
		`_ = "an environment"`,
		`_ = "the client's env: only PG names"`,
		`var b strings.Builder; fmt.Fprintf(&b, "x")`,
		`errorf(t, "x")`,
	} {
		source := "package backup\nimport (\"fmt\"; \"strings\"; \"testing\")\nvar _, _ = fmt.Sprint, strings.Repeat\nfunc TestX(t *testing.T) {\n" + body + "\n}\n"
		if problems := unfilteredOutput("sample_test.go", []byte(source)); len(problems) != 0 {
			t.Errorf("the guard flags %s: %q", body, problems)
		}
	}
}
