package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/link"
)

var terminalLanguages = []struct {
	name string
	env  string
	want i18n.Lang
}{
	{"English", "en", i18n.En},
	{"Chinese", "zh-Hans", i18n.ZhHans},
	{"Japanese", "ja", i18n.Ja},
	{"Unknown falls back to English", "fr", i18n.En},
}

func setTerminalLanguage(t *testing.T, language string) {
	t.Helper()
	t.Setenv("PERSONA_LANG", language)
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
}

func languageText(l i18n.Lang, zh, en, ja string) string {
	switch l {
	case i18n.ZhHans:
		return zh
	case i18n.Ja:
		return ja
	default:
		return en
	}
}

func TestStatusTerminalLanguagesAndChineseGolden(t *testing.T) {
	for _, tc := range terminalLanguages {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEnv(t)
			setTerminalLanguage(t, tc.env)
			if codeOf(status(f.env, nil)) != 1 {
				t.Fatal("an unpaired, stopped link is unhealthy")
			}
			want := languageText(tc.want,
				"模式：我去连 hub（主动）\n"+
					"链路：还没配对。在 hub 机器上运行 personad hub pair --machine <这台的 id> --add，再在这里 persona-link pair '<链接>'\n"+
					"后台服务：没装（persona-link install）\n",
				"Mode: Connect to the hub (active)\n"+
					"Link: not paired yet. On the hub, run personad hub pair --machine <this computer's id> --add, then run persona-link pair '<link>' here\n"+
					"Background service: not installed (persona-link install)\n",
				"モード：hub に接続（アクティブ）\n"+
					"リンク：まだペアリングしていません。hub で personad hub pair --machine <このコンピューターの id> --add を実行し、ここで persona-link pair '<リンク>' を実行してください\n"+
					"バックグラウンドサービス：未インストール（persona-link install）\n")
			want += languageText(tc.want,
				"日志："+f.logPath()+"（persona-link logs -f）\n",
				"Log: "+f.logPath()+" (persona-link logs -f)\n",
				"ログ："+f.logPath()+"（persona-link logs -f）\n")
			if f.out.String() != want {
				t.Fatalf("output:\n%s\nwant:\n%s", f.out, want)
			}
		})
	}
}

func TestStatusUsesLANGWhenPersonaLangIsUnset(t *testing.T) {
	f := newFakeEnv(t)
	setTerminalLanguage(t, "")
	t.Setenv("LANG", "en_US.UTF-8")
	_ = status(f.env, nil)
	if strings.Contains(f.out.String(), "模式：") || !strings.HasPrefix(f.out.String(), "Mode: Connect to the hub (active)\n") {
		t.Fatalf("LANG did not select English:\n%s", f.out)
	}
}

func TestUninstallTerminalLanguagesAndChineseGolden(t *testing.T) {
	for _, tc := range terminalLanguages {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEnv(t)
			setTerminalLanguage(t, tc.env)
			if err := uninstall(f.env, nil); err != nil {
				t.Fatal(err)
			}
			want := languageText(tc.want,
				"没装 systemd 用户服务（"+f.unitPath()+" 不存在）。\n"+
					"凭据还在 "+f.configPath()+"。这台不再连 hub 了：persona-link uninstall --purge，再在 App 的「设置 › 设备」里撤销它。\n",
				"No systemd user service is installed ("+f.unitPath()+" does not exist).\n"+
					"Credentials remain in "+f.configPath()+". To stop this computer from connecting to the hub, run persona-link uninstall --purge, then revoke it in the app under Settings › Devices.\n",
				"systemd ユーザーサービスはインストールされていません（"+f.unitPath()+" が存在しません）。\n"+
					"認証情報は "+f.configPath()+" に残っています。このコンピューターを hub に接続しないようにするには、persona-link uninstall --purge を実行し、アプリの「設定 › デバイス」で取り消してください。\n")
			if f.out.String() != want {
				t.Fatalf("output:\n%s\nwant:\n%s", f.out, want)
			}
		})
	}
}

func TestCommandTerminalLanguages(t *testing.T) {
	tests := []struct {
		name         string
		run          func(*fakeEnv) error
		explainError bool
		code         int
		zh, en, ja   func(*fakeEnv) string
	}{
		{
			name: "mode", run: func(f *fakeEnv) error { return modeCmd(f.env, nil) },
			zh: func(*fakeEnv) string { return "模式：我去连 hub（主动）\n" },
			en: func(*fakeEnv) string { return "Mode: Connect to the hub (active)\n" },
			ja: func(*fakeEnv) string { return "モード：hub に接続（アクティブ）\n" },
		},
		{
			name: "logs", run: func(f *fakeEnv) error { return logs(f.env, nil) },
			zh: func(f *fakeEnv) string { return "还没有日志（" + f.logPath() + " 不存在）。\n" },
			en: func(f *fakeEnv) string { return "No log yet (" + f.logPath() + " does not exist).\n" },
			ja: func(f *fakeEnv) string {
				return "ログはまだありません（" + f.logPath() + " が存在しません）。\n"
			},
		},
		{
			name: "ssh", run: func(f *fakeEnv) error { return sshCmd(f.env, nil) },
			zh: func(*fakeEnv) string { return "没有设 SSH 隧道（persona-link ssh <服务器> 设一个）。\n" },
			en: func(*fakeEnv) string { return "No SSH tunnel is set (set one with persona-link ssh <server>).\n" },
			ja: func(*fakeEnv) string {
				return "SSH トンネルは設定されていません（persona-link ssh <サーバー> で設定）。\n"
			},
		},
		{
			name: "node status", run: func(f *fakeEnv) error { return nodeCmd(f.env, []string{"status"}) }, code: 1,
			zh: func(f *fakeEnv) string {
				return "常驻 node：服务没装（persona-link node install）\n没有 " + f.nodeBin() + "\n"
			},
			en: func(f *fakeEnv) string {
				return "Resident node: service not installed (persona-link node install)\n" + f.nodeBin() + " is missing\n"
			},
			ja: func(f *fakeEnv) string {
				return "常駐 node：サービスは未インストールです（persona-link node install）\n" + f.nodeBin() + " がありません\n"
			},
		},
		{
			name: "pair validation", run: func(f *fakeEnv) error { return pairCmd(f.env, []string{"--listen", "--ssh", "example.com"}) }, explainError: true, code: -1,
			zh: func(*fakeEnv) string {
				return "pair --listen 不要 --ssh：SSH 隧道只在主动模式（我去连 hub）下用\n"
			},
			en: func(*fakeEnv) string {
				return "Do not use --ssh with pair --listen: SSH tunnels are only for active mode (this computer connects to the hub)\n"
			},
			ja: func(*fakeEnv) string {
				return "pair --listen には --ssh を指定しないでください。SSH トンネルはアクティブモード（このコンピューターから hub に接続）専用です\n"
			},
		},
		{
			name: "name validation", run: func(f *fakeEnv) error { return nameCmd(f.env, nil) }, explainError: true, code: -1,
			zh: func(*fakeEnv) string { return "还没配对：先 persona-link pair\n" },
			en: func(*fakeEnv) string { return "Not paired yet: run persona-link pair first\n" },
			ja: func(*fakeEnv) string {
				return "まだペアリングしていません。先に persona-link pair を実行してください\n"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, tc := range terminalLanguages {
				t.Run(tc.name, func(t *testing.T) {
					f := newFakeEnv(t)
					setTerminalLanguage(t, tc.env)
					err := test.run(f)
					if codeOf(err) != test.code {
						t.Fatalf("error = %v, code %d; want code %d", err, codeOf(err), test.code)
					}
					want := languageText(tc.want, test.zh(f), test.en(f), test.ja(f))
					if test.explainError {
						if err == nil {
							t.Fatal("expected an error")
						}
						if got := explainLanguage(tc.want, err) + "\n"; got != want {
							t.Fatalf("error = %q, want %q", got, want)
						}
					} else if f.out.String() != want {
						t.Fatalf("output = %q, want %q", f.out, want)
					}
				})
			}
		})
	}
}

func TestDiscoverTerminalLanguages(t *testing.T) {
	original := browseHubs
	browseHubs = func(_ context.Context, _ time.Duration) ([]foundHub, error) { return nil, nil }
	t.Cleanup(func() { browseHubs = original })
	for _, tc := range terminalLanguages {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEnv(t)
			setTerminalLanguage(t, tc.env)
			if codeOf(discover(f.env, nil)) != 1 {
				t.Fatal("no hub is an unsuccessful discovery")
			}
			want := languageText(tc.want,
				"3 秒内局域网里没听到 hub（mDNS；Docker 桥接网络里的 hub 听不到）。\n",
				"No hub heard on the LAN within 3s (mDNS; a hub in Docker's bridge network cannot be heard).\n",
				"3秒以内に LAN 内で hub を検出できませんでした（mDNS。Docker ブリッジネットワーク内の hub は検出できません）。\n")
			if f.out.String() != want {
				t.Fatalf("output = %q, want %q", f.out, want)
			}
		})
	}
}

func TestRunErrorLanguageSeparatesTerminalFromServiceLog(t *testing.T) {
	setTerminalLanguage(t, "ja")
	if got := runErrorLanguage(os.ModeCharDevice); got != i18n.Ja {
		t.Fatalf("foreground language = %q", got)
	}
	if got := runErrorLanguage(0); got != i18n.En {
		t.Fatalf("redirected language = %q", got)
	}
	if got := explainLanguage(runErrorLanguage(0), link.ErrRunning); got != "Another Persona Link is already running on this computer" {
		t.Fatalf("service log error = %q", got)
	}
	if got := explainLanguage(runErrorLanguage(os.ModeCharDevice), link.ErrRunning); got != "別の Persona Link がこのコンピューターですでに実行されています" {
		t.Fatalf("terminal error = %q", got)
	}
}

func TestWindowsPortableMessageSelection(t *testing.T) {
	for _, tc := range terminalLanguages {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEnv(t)
			setTerminalLanguage(t, tc.env)
			state := &serviceState{Manager: "schtasks", Unit: taskName, File: filepath.Join(f.home, "task.xml"), Active: "registered"}
			var out strings.Builder
			taskText(state, func(format string, args ...any) { fmt.Fprintf(&out, format+"\n", args...) })
			want := languageText(tc.want,
				"后台服务：计划任务「Persona Link」· registered · 登录时启动\n",
				"Background service: scheduled task “Persona Link” · registered · starts at sign-in\n",
				"バックグラウンドサービス：スケジュールされたタスク「Persona Link」· 登録済み · サインイン時に起動\n")
			if out.String() != want || state.Active != "registered" {
				t.Fatalf("output = %q, state = %+v", out.String(), state)
			}
		})
	}
}

func TestUsageTerminalLanguages(t *testing.T) {
	for _, tc := range terminalLanguages {
		t.Run(tc.name, func(t *testing.T) {
			setTerminalLanguage(t, tc.env)
			got := usage()
			header := languageText(tc.want, "用法：\n", "Usage:\n", "使用法：\n")
			description := languageText(tc.want,
				"两种模式的配对各自留着，切回去不用重新配对",
				"Each mode keeps its own pairing, so switching back does not require pairing again.",
				"各モードのペアリングを保持するため、戻るときに再ペアリングは不要です。")
			if !strings.HasPrefix(got, header) || !strings.Contains(got, description) || strings.Contains(got, "link-cli.usage") {
				t.Fatalf("usage in %s:\n%s", tc.env, got)
			}
		})
	}
}

func TestWindowsPersonadErrorTerminalLanguages(t *testing.T) {
	for _, tc := range terminalLanguages {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEnv(t)
			setTerminalLanguage(t, tc.env)
			f.goos = "windows"
			node := filepath.Join(t.TempDir(), "persona-node")
			if err := os.WriteFile(node, []byte("#!/bin/sh\n# persona-node\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			f.onCommand = func(string) (string, error) { return `{"program":"persona-node"}`, nil }
			err := install(f.env, []string{"--node-bin", node})
			if err == nil {
				t.Fatal("persona-node was accepted for a Windows node")
			}
			want := languageText(tc.want,
				node+" is not personad: on Windows the node runs personad (docs/operations/windows.md); give personad with --personad",
				node+" is not personad: the Windows node runs personad (docs/operations/windows.md); provide personad with --personad",
				node+" は personad ではありません。Windows の node には personad が必要です（docs/operations/windows.md）。--personad で personad を指定してください")
			if got := explainLanguage(tc.want, err); got != want {
				t.Fatalf("error = %q, want %q", got, want)
			}
		})
	}
}

func TestLegacyEnglishMessagesStayEnglishInChinese(t *testing.T) {
	f := newFakeEnv(t)
	setTerminalLanguage(t, "zh-Hans")
	if got := explainLanguage(i18n.ZhHans, link.NewTextError("link-cli.supervise_not_windows", nil)); got != "supervise 只在 Windows 上用（计划任务里跑它，见 persona-link install）" {
		t.Fatalf("supervise = %q", got)
	}
	path := filepath.Join(f.home, "persona-node")
	want := path + " is not personad: on Windows the node runs personad (docs/operations/windows.md); give personad with --personad"
	if got := explainLanguage(i18n.ZhHans, link.NewTextError("link-cli.windows_personad_required", nil, i18n.Args{"path": path, "program": "persona-node"})); got != want {
		t.Fatalf("Windows personad error = %q, want %q", got, want)
	}
	for _, tc := range []struct{ key, want string }{
		{"node_program_unknown", path + " does not say which program it is (node-update-check did not answer here): give personad or persona-node built for this computer"},
		{"node_wrong_flag", path + " is persona-node, not personad: give it with --node-bin"},
		{"node_program_not_runnable", path + " does not run here"},
		{"node_hub_requires_personad", "this computer runs a hub or a manager, whose node runs personad: " + path + " is persona-node; install personad (the install command with --hub, or the manager's install command)"},
		{"node_switch_hint", "It runs personad. To have it run the device's own program (" + path + "): persona-link node switch"},
		{"node_switch_pending", "A node switch did not finish: persona-link run (within half a minute), or the next node install or switch, puts the old service back."},
		{"node_status_runs", "It runs persona-node"},
	} {
		if got := explainLanguage(i18n.ZhHans, link.NewTextError("link-cli."+tc.key, nil, i18n.Args{"path": path, "program": "persona-node"})); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestDurationLanguages(t *testing.T) {
	for _, tc := range terminalLanguages {
		t.Run(tc.name, func(t *testing.T) {
			setTerminalLanguage(t, tc.env)
			for _, test := range []struct {
				d          time.Duration
				zh, en, ja string
			}{
				{-time.Second, "0 秒", "0s", "0秒"},
				{time.Second, "1 秒", "1s", "1秒"},
				{59 * time.Second, "59 秒", "59s", "59秒"},
				{time.Minute, "1 分钟", "1m", "1分"},
				{59 * time.Minute, "59 分钟", "59m", "59分"},
				{time.Hour + time.Minute, "1 小时 1 分钟", "1h 1m", "1時間1分"},
				{24*time.Hour + time.Hour, "1 天 1 小时", "1d 1h", "1日1時間"},
			} {
				if got, want := duration(test.d), languageText(tc.want, test.zh, test.en, test.ja); got != want {
					t.Errorf("duration(%s) = %q, want %q", test.d, got, want)
				}
			}
		})
	}
}

func TestJoinedCandidateDelimiterRemainsCanonical(t *testing.T) {
	setTerminalLanguage(t, "en")
	parts := errorParts("内网 192.0.2.10:18662：超时；域名 b：peer 原文：keep；untouched")
	if len(parts) != 3 || parts[1] != "域名 b：peer 原文：keep" || parts[2] != "untouched" {
		t.Fatalf("parts = %#v", parts)
	}
	if got := link.StatusErrorText(i18n.En, parts[0]); got != "LAN 192.0.2.10:18662: timed out" {
		t.Fatalf("known candidate = %q", got)
	}
	for _, part := range parts[1:] {
		if got := link.StatusErrorText(i18n.En, part); got != part {
			t.Fatalf("unknown peer text changed: %q", got)
		}
	}
}

func TestSystemdMissingKeepsLegacyJSONPunctuation(t *testing.T) {
	for _, tc := range terminalLanguages {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEnv(t)
			setTerminalLanguage(t, tc.env)
			f.onCommand = func(string) (string, error) { return "", errors.New("exit status 1") }
			r := gather(f.env)
			want := languageText(tc.want,
				"连不上你的 systemd 用户实例：exit status 1",
				"Cannot reach your systemd user instance：exit status 1",
				"systemd ユーザーインスタンスに接続できません：exit status 1")
			if r.Service == nil || r.Service.Missing != want {
				t.Fatalf("service.missing = %q, want %q", r.Service.Missing, want)
			}
		})
	}
}

func TestStatusJSONAndSavedBytesAreLanguageInvariant(t *testing.T) {
	f := newFakeEnv(t)
	f.pairHere(t)
	release, err := link.LockRun(f.configPath())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	retry := f.now().Add(12 * time.Second)
	canonicalError := "内网 192.0.2.10:18662：超时；域名 hub.example.com:18662：peer 原文：keep"
	f.saveStatus(t, link.Status{State: "waiting", Machine: "pi", HubName: "家里", Retry: &retry, Error: canonicalError}, 4321)
	statusPath := link.StatusPath(f.configPath())
	savedBefore, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	var machine []byte
	for _, language := range []string{"zh-Hans", "en", "ja", "fr"} {
		setTerminalLanguage(t, language)
		f.out.Reset()
		_ = status(f.env, nil) // Exercise the human presentation before reading the same machine state.
		f.out.Reset()
		if codeOf(status(f.env, []string{"--json"})) != 1 {
			t.Fatal("waiting is not healthy")
		}
		if machine == nil {
			machine = append([]byte(nil), f.out.Bytes()...)
		} else if !bytes.Equal(f.out.Bytes(), machine) {
			t.Fatalf("%s changed status JSON:\n%s\nwant:\n%s", language, f.out, machine)
		}
	}
	if !bytes.Contains(machine, []byte(`"error": "内网 192.0.2.10:18662：超时；域名 hub.example.com:18662：peer 原文：keep"`)) {
		t.Fatalf("canonical saved error is not in JSON:\n%s", machine)
	}
	savedAfter, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(savedAfter, savedBefore) {
		t.Fatal("human rendering changed link.status.json")
	}
}

func TestMalformedConfigJSONAndHumanRendering(t *testing.T) {
	f := newFakeEnv(t)
	if err := os.MkdirAll(filepath.Dir(f.configPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.configPath(), []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	const detail = "invalid character 'b' looking for beginning of object key string"
	canonical := f.configPath() + ": " + detail
	var machine []byte
	for _, language := range []string{"zh-Hans", "en", "ja", "fr"} {
		setTerminalLanguage(t, language)
		r := gather(f.env)
		if r.Error != canonical {
			t.Fatalf("%s canonical config error = %q, want %q", language, r.Error, canonical)
		}
		human := r.text(f.now(), "me")
		switch language {
		case "zh-Hans":
			if !strings.Contains(human, "凭据：读不了（"+canonical+"）") {
				t.Fatalf("legacy Chinese rendering = %q", human)
			}
		case "en", "fr":
			if !strings.Contains(human, "Credentials: cannot read them (Cannot read "+canonical+")") {
				t.Fatalf("English rendering = %q", human)
			}
		case "ja":
			if !strings.Contains(human, "認証情報：読み取れません（"+f.configPath()+" を読み取れません："+detail+"）") {
				t.Fatalf("Japanese rendering = %q", human)
			}
		}
		assertNoTerminalPlaceholder(t, human)
		f.out.Reset()
		_ = status(f.env, []string{"--json"})
		if machine == nil {
			machine = append([]byte(nil), f.out.Bytes()...)
		} else if !bytes.Equal(f.out.Bytes(), machine) {
			t.Fatalf("%s changed malformed-config JSON", language)
		}
	}
}

func TestConfigErrorPresentationDoesNotChangeCanonicalBytes(t *testing.T) {
	setTerminalLanguage(t, "en")
	path := filepath.Join(t.TempDir(), "link.json")
	if err := os.WriteFile(path, []byte(`{"machine":"pi"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := link.Load(path)
	if err == nil {
		t.Fatal("incomplete config loaded")
	}
	canonical := path + " is incomplete: pair again (persona-link pair)"
	if err.Error() != canonical || link.ErrorText(i18n.ZhHans, err) != canonical {
		t.Fatalf("canonical error = %q", err)
	}
	if got := link.ErrorText(i18n.En, err); got != path+" is incomplete: pair again with persona-link pair" {
		t.Fatalf("English error = %q", got)
	}
	if got := link.ErrorText(i18n.Ja, err); !strings.HasPrefix(got, path+" が不完全です。") {
		t.Fatalf("Japanese error = %q", got)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatal("an incomplete config is not a missing config")
	}
}
