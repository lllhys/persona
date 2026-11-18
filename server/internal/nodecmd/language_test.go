package nodecmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeLanguageChild(t *testing.T) {
	raw := os.Getenv("PERSONA_TEST_NODE_LANGUAGE")
	if raw == "" {
		return
	}
	handled, err := Run(context.Background(), func() {}, strings.Split(raw, "\x1f"), Build{})
	if !handled {
		os.Exit(99)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestNodeMessagesSpeakTheTerminalLanguage(t *testing.T) {
	languages := []struct {
		name, setting, language string
	}{
		{name: "en", setting: "en", language: "en"},
		{name: "zh-Hans", setting: "zh-Hans", language: "zh-Hans"},
		{name: "ja", setting: "ja", language: "ja"},
		{name: "unknown falls back to en", setting: "ko", language: "en"},
	}
	tests := []struct {
		name  string
		args  []string
		reset bool
		lines map[string]string
	}{
		{
			name: "unknown subcommand", args: []string{"node", "unknown"},
			lines: map[string]string{
				"en":      `Unknown node subcommand "unknown"; available: --stdio, --attach, tailcat, doctor, reset-uid (see personad node -h)`,
				"zh-Hans": `不认识的 node 子命令 "unknown"：可用的有 --stdio、--attach、tailcat、doctor、reset-uid（personad node -h 看用法）`,
				"ja":      `nodeのサブコマンド"unknown"が認識できません。使用できるのは--stdio、--attach、tailcat、doctor、reset-uidです（personad node -hで使用方法を確認）`,
			},
		},
		{
			name: "extra argument after reset-uid", args: []string{"node", "reset-uid", "extra"},
			lines: map[string]string{
				"en":      `node reset-uid takes no arguments; got "extra" (see personad node -h)`,
				"zh-Hans": `node reset-uid 不带参数，多了 "extra"（personad node -h 看用法）`,
				"ja":      `node reset-uidは引数を取りません。余分な引数："extra"（personad node -hで使用方法を確認）`,
			},
		},
		{
			name: "tailcat alone", args: []string{"node", "tailcat"},
			lines: map[string]string{
				"en":      `node tailcat requires init or addr (see personad node -h)`,
				"zh-Hans": `node tailcat 要跟 init 或 addr（personad node -h 看用法）`,
				"ja":      `node tailcatにはinitまたはaddrを指定してください（personad node -hで使用方法を確認）`,
			},
		},
		{
			name: "unknown tailcat subcommand", args: []string{"node", "tailcat", "unknown"},
			lines: map[string]string{
				"en":      `Unknown node tailcat subcommand "unknown"; available: init, addr (see personad node -h)`,
				"zh-Hans": `不认识的 node tailcat 子命令 "unknown"：可用的有 init、addr（personad node -h 看用法）`,
				"ja":      `node tailcatのサブコマンド"unknown"が認識できません。使用できるのはinit、addrです（personad node -hで使用方法を確認）`,
			},
		},
		{
			name: "extra argument to tailcat init", args: []string{"node", "tailcat", "init", "--hub", "key", "extra"},
			lines: map[string]string{
				"en":      `node tailcat init accepts only --hub and --derp-map-url; got "extra" (see personad node -h)`,
				"zh-Hans": `node tailcat init 只认 --hub 和 --derp-map-url，多了 "extra"（personad node -h 看用法）`,
				"ja":      `node tailcat initで使用できるのは--hubと--derp-map-urlのみです。余分な引数："extra"（personad node -hで使用方法を確認）`,
			},
		},
		{
			name: "reset-uid", args: []string{"node", "reset-uid"}, reset: true,
			lines: map[string]string{
				"en":      `Assigned a new machine uid. Restart the resident node on this computer (scripts/deploy/node-launchd.sh install restarts it); the hub will use the new uid the next time it connects.`,
				"zh-Hans": `已换成新的机器 uid。重启这台电脑上的常驻 node（scripts/deploy/node-launchd.sh install 会重启它），hub 下次连上时就用新的。`,
				"ja":      `新しいマシンuidに変更しました。このコンピューター上の常駐ノードを再起動してください（scripts/deploy/node-launchd.sh installでも再起動できます）。ハブは次回接続時に新しいuidを使用します。`,
			},
		},
	}
	for _, language := range languages {
		for _, tc := range tests {
			t.Run(language.name+"/"+tc.name, func(t *testing.T) {
				home := t.TempDir()
				cmd := exec.Command(os.Args[0], "-test.run=^TestNodeLanguageChild$")
				cmd.Env = append(os.Environ(),
					"PERSONA_TEST_NODE_LANGUAGE="+strings.Join(tc.args, "\x1f"),
					"HOME="+home, "PERSONA_HOME="+filepath.Join(home, ".persona"),
					"PERSONA_LANG="+language.setting, "LC_ALL=", "LC_MESSAGES=", "LANG=",
				)
				var stdout, stderr strings.Builder
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				err := cmd.Run()
				code := 0
				if exit, ok := err.(*exec.ExitError); ok {
					code = exit.ExitCode()
				} else if err != nil {
					t.Fatal(err)
				}
				wantCode := 2
				wantErr := tc.lines[language.language] + "\n" + Named(NodeUsage) + "\n"
				if tc.reset {
					wantCode = 0
					wantErr = tc.lines[language.language] + "\n"
				}
				if code != wantCode || stderr.String() != wantErr {
					t.Fatalf("exit %d, stderr %q; want exit %d, stderr %q (stdout %q)", code, stderr.String(), wantCode, wantErr, stdout.String())
				}
				if !tc.reset {
					if stdout.String() != "" {
						t.Fatalf("stdout %q, want nothing", stdout.String())
					}
					return
				}
				uid := strings.TrimSpace(stdout.String())
				if uid == "" || stdout.String() != uid+"\n" {
					t.Fatalf("stdout %q, want one machine uid", stdout.String())
				}
				stored, err := os.ReadFile(filepath.Join(home, ".persona", "machine-uid"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.TrimSpace(string(stored)) != uid {
					t.Fatalf("stdout uid %q, stored uid %q", uid, stored)
				}
			})
		}
	}
}

func TestLaunchUsageNamesTheEnglishSettingsPage(t *testing.T) {
	const want = `  personad launch <name> …   run claude with a launcher kept in Settings › Models (from the local node)`
	if LaunchUsage != want {
		t.Fatalf("LaunchUsage = %q, want %q", LaunchUsage, want)
	}
}
