package node

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

func TestNodePackageInitDoesNotTranslate(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				for _, value := range spec.(*ast.ValueSpec).Values {
					ast.Inspect(value, func(n ast.Node) bool {
						if _, lazy := n.(*ast.FuncLit); lazy {
							return false // a function body runs at its call site, not during package initialization
						}
						call, ok := n.(*ast.CallExpr)
						if !ok {
							return true
						}
						sel, ok := call.Fun.(*ast.SelectorExpr)
						if !ok {
							return true
						}
						pkg, ok := sel.X.(*ast.Ident)
						if ok && pkg.Name == "i18n" && sel.Sel.Name == "T" {
							t.Errorf("%s: package initialization calls i18n.T", fset.Position(call.Pos()))
						}
						return true
					})
				}
			}
		}
	}

	// The constants exist only for legacy comparisons; their bytes still match the catalog used by actual checks.
	if launchdFixHint != i18n.T(i18n.Default, launchdFixHintKey) ||
		launchdFixHintInside != i18n.T(i18n.Default, launchdFixHintInsideKey) {
		t.Error("legacy launchd hint constants differ from the default catalog wording")
	}
}

func TestLaunchAgentReportSpeaksEveryLanguage(t *testing.T) {
	for _, tc := range []struct {
		name        string
		lang        i18n.Lang
		detail, fix string
		hint        string
	}{
		{name: "English", lang: i18n.En, detail: "The resident-node LaunchAgent has no ProcessType", fix: "this fix left it unchanged", hint: "Run personad node doctor --fix in a terminal on this Mac"},
		{name: "Chinese", lang: i18n.ZhHans, detail: "常驻 node 的 LaunchAgent 没写 ProcessType", fix: "这次修复没动它", hint: launchdFixHintInside},
		{name: "Japanese", lang: i18n.Ja, detail: "常駐ノードのLaunchAgentにProcessTypeがありません", fix: "今回の修復では変更していません", hint: "このMacのターミナルでpersonad node doctor --fixを実行してください"},
		{name: "unknown resolved to English", lang: i18n.Resolve("de"), detail: "The resident-node LaunchAgent has no ProcessType", fix: "this fix left it unchanged", hint: "Run personad node doctor --fix in a terminal on this Mac"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := checkLaunchAgent(launchAgent{exists: true}, DoctorOptions{Lang: tc.lang, LaunchAgent: "/persona-test/agent.plist", InsideNode: true, Fix: true})
			if c.Name != launchdCheck || c.Status != DoctorWarn || len(c.Items) != 1 || c.Items[0] != "/persona-test/agent.plist" {
				t.Errorf("%+v", c)
			}
			for _, want := range []string{tc.detail, tc.fix} {
				if !strings.Contains(c.Detail, want) {
					t.Errorf("detail %q does not contain %q", c.Detail, want)
				}
			}
			if !strings.Contains(c.Hint, tc.hint) {
				t.Errorf("hint %q does not contain %q", c.Hint, tc.hint)
			}
		})
	}
}

func TestLaunchdRestartAndBootstrapWords(t *testing.T) {
	for _, tc := range []struct {
		name, detail, hint, bootstrap string
		lang                          i18n.Lang
	}{
		{name: "English", lang: i18n.En, detail: "has not answered within 300ms", hint: "See its log at /persona-test/node.log", bootstrap: "launchctl bootstrap failed"},
		{name: "Chinese", lang: i18n.ZhHans, detail: "300ms 内还没有应答", hint: "原因看它的日志 /persona-test/node.log", bootstrap: "launchctl bootstrap 失败"},
		{name: "Japanese", lang: i18n.Ja, detail: "300ms以内に応答がありません", hint: "ログを確認してください：/persona-test/node.log", bootstrap: "launchctl bootstrapに失敗しました"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep := DoctorReport{Checks: []DoctorCheck{{Name: "node.sock", Status: DoctorFail, Detail: i18n.T(tc.lang, "node-doctor.socket_missing")}}}
			notBackYet(&rep, []string{"node.sock"}, 300*time.Millisecond, "/persona-test/node.log", tc.lang)
			if !strings.Contains(rep.Checks[0].Detail, tc.detail) || !strings.Contains(rep.Checks[0].Hint, tc.hint) {
				t.Errorf("%+v", rep.Checks[0])
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // one failed bootstrap is enough; do not wait through the production retry budget
			opts := DoctorOptions{Lang: tc.lang, LaunchAgent: "/persona-test/agent.plist", LaunchdTarget: "gui/501/com.persona.node",
				Launchctl: func(context.Context, ...string) (string, error) {
					return "Input/output error", errors.New("exit status 5")
				}}
			err := reloadLaunchAgent(ctx, opts)
			if err == nil || !strings.Contains(err.Error(), tc.bootstrap) || !strings.Contains(err.Error(), "Input/output error") ||
				!strings.Contains(err.Error(), "launchctl bootstrap gui/501 /persona-test/agent.plist") {
				t.Errorf("bootstrap error = %v", err)
			}
		})
	}
}

func TestDoctorWrappedErrorKeepsItsCause(t *testing.T) {
	cause := errors.New("read-only disk")
	err := wrapDoctorError(i18n.En, "node-doctor.launchd_rewrite_failed", i18n.Args{"path": "/persona-test/agent.plist"}, cause)
	if !errors.Is(err, cause) {
		t.Fatal("the localized error no longer unwraps to its cause")
	}
	if got, want := err.Error(), "Could not rewrite /persona-test/agent.plist: read-only disk"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}
