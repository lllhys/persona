package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/link"
)

var (
	terminalCJK         = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}]`)
	terminalPlaceholder = regexp.MustCompile(`\{[0-9A-Za-z_]+\}`)
)

func assertNoTerminalPlaceholder(t *testing.T, text string) {
	t.Helper()
	if match := terminalPlaceholder.FindString(text); match != "" {
		t.Errorf("unfilled terminal placeholder %q in:\n%s", match, text)
	}
}

func assertEnglishTerminalText(t *testing.T, text string) {
	t.Helper()
	assertNoTerminalPlaceholder(t, text)
	if match := terminalCJK.FindString(text); match != "" {
		t.Errorf("CJK text %q in English output:\n%s", match, text)
	}
}

func commandPresentation(f *fakeEnv, err error) string {
	text := f.out.String()
	var code exitCode
	if err != nil && !errors.As(err, &code) && !errors.Is(err, errUsage) {
		text += "\n" + explainLanguage(i18n.En, err)
	}
	return text
}

func saveLiveStatusForLanguageTest(t *testing.T, f *fakeEnv, status link.Status) {
	t.Helper()
	release, err := link.LockRun(f.configPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	f.saveStatus(t, status, 42)
}

func setPassiveConfigForLanguageTest(t *testing.T, f *fakeEnv, paired bool) {
	t.Helper()
	f.pairHere(t)
	if _, err := link.SetMode(f.configPath(), link.ModePassive, ":18663"); err != nil {
		t.Fatal(err)
	}
	if !paired {
		return
	}
	pairedAt := f.now().Add(-time.Hour)
	if err := link.Update(f.configPath(), func(c *link.Config) error {
		c.Passive.TokenSHA256 = "hash"
		c.Passive.HubID = "hub-id"
		c.Passive.HubName = "Hub"
		c.Passive.Machine = "pi"
		c.Passive.MachineName = "Computer"
		c.Passive.PairedAt = &pairedAt
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func installWithReportForLanguageTest(t *testing.T, f *fakeEnv, status link.Status) error {
	t.Helper()
	f.onCommand = func(call string) (string, error) {
		if strings.HasSuffix(call, "restart persona-link.service") {
			f.saveStatus(t, status, 42)
		}
		return "", nil
	}
	return install(f.env, []string{"--no-node"})
}

func TestEnglishCommandPathsHaveNoCJKOrUnfilledPlaceholders(t *testing.T) {
	candidateErrors := "内网 192.0.2.10:18662：超时；域名 hub.example.com:18662：拒绝连接"
	nodeDown := link.NodeDownSelf + link.NodeDownMarker + "（127.0.0.1:1 连不上）：persona-link node install（systemd 用户服务）"
	cases := []struct {
		name string
		run  func(*testing.T, *fakeEnv) error
	}{
		{"install without a systemd boot", func(t *testing.T, f *fakeEnv) error {
			f.systemd = false
			return install(f.env, []string{"--no-node"})
		}},
		{"install with a node and no systemd boot", func(t *testing.T, f *fakeEnv) error {
			f.systemd = false
			return install(f.env, nil)
		}},
		{"install without systemctl", func(t *testing.T, f *fakeEnv) error {
			f.lookPath = func(string) (string, error) { return "", errors.New("not found") }
			return install(f.env, []string{"--no-node"})
		}},
		{"install without a systemd user instance", func(t *testing.T, f *fakeEnv) error {
			f.onCommand = func(string) (string, error) {
				return "Failed to connect to bus: No medium found", errors.New("exit status 1")
			}
			return install(f.env, []string{"--no-node"})
		}},
		{"uninstall without systemd", func(t *testing.T, f *fakeEnv) error {
			if err := os.MkdirAll(filepath.Dir(f.unitPath()), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.unitPath(), []byte("[Unit]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			f.systemd = false
			return uninstall(f.env, nil)
		}},
		{"node install without systemd", func(t *testing.T, f *fakeEnv) error {
			if err := os.MkdirAll(filepath.Dir(f.nodeBin()), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.nodeBin(), []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			f.systemd = false
			return nodeCmd(f.env, []string{"install"})
		}},
		{"status without a systemd boot", func(t *testing.T, f *fakeEnv) error {
			f.systemd = false
			return status(f.env, nil)
		}},
		{"status without systemctl", func(t *testing.T, f *fakeEnv) error {
			f.lookPath = func(string) (string, error) { return "", errors.New("not found") }
			return status(f.env, nil)
		}},
		{"status on another system", func(t *testing.T, f *fakeEnv) error {
			f.goos = "freebsd"
			return status(f.env, nil)
		}},
		{"install on another system", func(t *testing.T, f *fakeEnv) error {
			f.goos = "freebsd"
			return install(f.env, nil)
		}},
		{"node status on another system", func(t *testing.T, f *fakeEnv) error {
			f.goos = "freebsd"
			return nodeCmd(f.env, []string{"status"})
		}},
		{"mode", func(t *testing.T, f *fakeEnv) error { return modeCmd(f.env, nil) }},
		{"uninstall without a unit", func(t *testing.T, f *fakeEnv) error { return uninstall(f.env, nil) }},
		{"node status", func(t *testing.T, f *fakeEnv) error { return nodeCmd(f.env, []string{"status"}) }},
		{"logs without a file", func(t *testing.T, f *fakeEnv) error { return logs(f.env, nil) }},
		{"ssh without a tunnel", func(t *testing.T, f *fakeEnv) error { return sshCmd(f.env, nil) }},
		{"pair flag validation", func(t *testing.T, f *fakeEnv) error {
			return pairCmd(f.env, []string{"--listen", "--ssh", "example.com"})
		}},
		{"name before pairing", func(t *testing.T, f *fakeEnv) error { return nameCmd(f.env, nil) }},
		{"discover wait validation", func(t *testing.T, f *fakeEnv) error { return discover(f.env, []string{"--wait", "0"}) }},
		{"supervise on another system", func(t *testing.T, f *fakeEnv) error { return supervise(f.env, nil) }},
		{"reconnect while stopped", func(t *testing.T, f *fakeEnv) error { return reconnectNow(f.env, nil) }},
		{"mode passive", func(t *testing.T, f *fakeEnv) error { return modeCmd(f.env, []string{"passive"}) }},
		{"pair listen", func(t *testing.T, f *fakeEnv) error {
			return pairCmd(f.env, []string{"--listen", "--addr", "example.com"})
		}},
		{"invalid address", func(t *testing.T, f *fakeEnv) error { return address([]string{"add", "ftp://x"}) }},
		{"ssh without a server", func(t *testing.T, f *fakeEnv) error { return sshCmd(f.env, []string{"--target", "127.0.0.1:1"}) }},
		{"help", func(t *testing.T, f *fakeEnv) error {
			_, _ = f.out.WriteString(usage())
			return nil
		}},
		{"status with malformed config", func(t *testing.T, f *fakeEnv) error {
			if err := os.MkdirAll(filepath.Dir(f.configPath()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.configPath(), []byte(`{broken`), 0o600); err != nil {
				t.Fatal(err)
			}
			return status(f.env, nil)
		}},
		{"status waiting with candidate errors", func(t *testing.T, f *fakeEnv) error {
			f.pairHere(t)
			saveLiveStatusForLanguageTest(t, f, link.Status{State: "waiting", Machine: "pi", HubName: "Hub", Error: candidateErrors})
			return status(f.env, nil)
		}},
		{"status refused", func(t *testing.T, f *fakeEnv) error {
			f.pairHere(t)
			saveLiveStatusForLanguageTest(t, f, link.Status{State: "refused", Machine: "pi", HubName: "Hub", Error: "配对失败：hub 拒绝了：401 Unauthorized"})
			return status(f.env, nil)
		}},
		{"status connected with the node down", func(t *testing.T, f *fakeEnv) error {
			f.pairHere(t)
			saveLiveStatusForLanguageTest(t, f, link.Status{State: "connected", Machine: "pi", HubName: "Hub", Kind: "lan", Address: "https://192.0.2.10:18662", Node: "offline", NodeError: nodeDown})
			return status(f.env, nil)
		}},
		{"status connected with another node error", func(t *testing.T, f *fakeEnv) error {
			f.pairHere(t)
			saveLiveStatusForLanguageTest(t, f, link.Status{State: "connected", Machine: "pi", HubName: "Hub", Kind: "lan", Address: "https://192.0.2.10:18662", Node: "offline", NodeError: "dial unix /tmp/node.sock: permission denied"})
			return status(f.env, nil)
		}},
		{"status passive with a listen error", func(t *testing.T, f *fakeEnv) error {
			setPassiveConfigForLanguageTest(t, f, false)
			saveLiveStatusForLanguageTest(t, f, link.Status{Mode: link.ModePassive, State: "waiting", Listen: ":18663", Error: ":18663：端口被占用了"})
			return status(f.env, nil)
		}},
		{"status passive connected", func(t *testing.T, f *fakeEnv) error {
			setPassiveConfigForLanguageTest(t, f, true)
			lastSeen := f.now().Add(-time.Minute)
			saveLiveStatusForLanguageTest(t, f, link.Status{Mode: link.ModePassive, State: "connected", Machine: "pi", HubName: "Hub", Listen: ":18663", Address: "192.0.2.20:18663", From: "192.0.2.20:18663", LastSeen: &lastSeen, Addrs: []string{"192.0.2.10:18663"}, Node: "online"})
			return status(f.env, nil)
		}},
		{"status not running with a last report", func(t *testing.T, f *fakeEnv) error {
			f.pairHere(t)
			f.saveStatus(t, link.Status{State: "waiting", Machine: "pi", HubName: "Hub", Error: candidateErrors}, 41)
			f.sleep(2 * time.Minute)
			return status(f.env, nil)
		}},
		{"status with an unavailable systemd user instance", func(t *testing.T, f *fakeEnv) error {
			if err := os.MkdirAll(filepath.Dir(f.unitPath()), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.unitPath(), []byte("[Unit]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			f.onCommand = func(string) (string, error) {
				return "Failed to connect to bus: No medium found", errors.New("exit status 1")
			}
			return status(f.env, nil)
		}},
		{"install wait with candidate errors", func(t *testing.T, f *fakeEnv) error {
			f.pairHere(t)
			return installWithReportForLanguageTest(t, f, link.Status{State: "waiting", Machine: "pi", HubName: "Hub", Error: candidateErrors})
		}},
		{"install wait refused", func(t *testing.T, f *fakeEnv) error {
			f.pairHere(t)
			return installWithReportForLanguageTest(t, f, link.Status{State: "refused", Machine: "pi", HubName: "Hub", Error: "配对失败：hub 拒绝了：401 Unauthorized"})
		}},
		{"install wait with the node down", func(t *testing.T, f *fakeEnv) error {
			f.pairHere(t)
			return installWithReportForLanguageTest(t, f, link.Status{State: "connected", Machine: "pi", HubName: "Hub", Kind: "lan", Address: "https://192.0.2.10:18662", Node: "offline", NodeError: nodeDown})
		}},
		{"install wait with another node error", func(t *testing.T, f *fakeEnv) error {
			f.pairHere(t)
			return installWithReportForLanguageTest(t, f, link.Status{State: "connected", Machine: "pi", HubName: "Hub", Kind: "lan", Address: "https://192.0.2.10:18662", Node: "offline", NodeError: "dial unix /tmp/node.sock: permission denied"})
		}},
		{"install wait passive with a listen error", func(t *testing.T, f *fakeEnv) error {
			setPassiveConfigForLanguageTest(t, f, false)
			return installWithReportForLanguageTest(t, f, link.Status{Mode: link.ModePassive, State: "waiting", Listen: ":18663", Error: ":18663：端口被占用了"})
		}},
		{"install wait passive connected", func(t *testing.T, f *fakeEnv) error {
			setPassiveConfigForLanguageTest(t, f, true)
			return installWithReportForLanguageTest(t, f, link.Status{Mode: link.ModePassive, State: "connected", Machine: "pi", HubName: "Hub", Listen: ":18663", Address: "192.0.2.20:18663", Node: "online"})
		}},
		{"Windows task texts", func(t *testing.T, f *fakeEnv) error {
			for _, state := range []*serviceState{
				{Manager: "schtasks", Unit: taskName, File: f.taskFile()},
				{Manager: "schtasks", Unit: taskName, File: f.taskFile(), Installed: true},
				{Manager: "schtasks", Unit: taskName, File: f.taskFile(), Installed: true, Active: "Running"},
				{Manager: "schtasks", Unit: taskName, File: f.taskFile(), Installed: true, Active: "registered"},
			} {
				taskText(state, func(format string, args ...any) { fmt.Fprintf(f.out, format+"\n", args...) })
			}
			return nil
		}},
		{"binary install failure", func(t *testing.T, f *fakeEnv) error {
			return link.NewTextError("link-cli.binary_install_failed", errors.New("disk full"), i18n.Args{"path": f.binPath()})
		}},
		{"explain a rejected link", func(t *testing.T, f *fakeEnv) error {
			return link.NewTextError("link-errors.pair_failed", errors.New("hub 拒绝了：invalid token"))
		}},
		{"explain an unreachable hub", func(t *testing.T, f *fakeEnv) error {
			cause := &url.Error{Op: "Post", URL: "https://192.0.2.10:18662/api/v1/auth/tokens", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}}
			return link.NewTextError("link-errors.pair_failed", cause)
		}},
		{"explain an SSH error", func(t *testing.T, f *fakeEnv) error {
			return &link.SSHError{Code: link.SSHHostKey, Server: "user@example.com", Target: "127.0.0.1:1", Err: errors.New("host key changed")}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEnv(t)
			setTerminalLanguage(t, "en")
			err := tc.run(t, f)
			assertEnglishTerminalText(t, commandPresentation(f, err))
		})
	}
}

func TestSystemdReasonUsesTerminalLanguageAtEveryBoundary(t *testing.T) {
	const (
		bootZH = "系统不是由 systemd 启动的（容器里常见）"
		bootEN = "the system was not booted by systemd (common in containers)"
		bootJA = "システムが systemd から起動されていません（コンテナーでよくあります）"
	)
	cases := []struct {
		name       string
		raw        string
		setup      func(*testing.T, *fakeEnv)
		run        func(*fakeEnv) error
		want       func(i18n.Lang, *fakeEnv) string
		prefix     bool
		exactError bool
	}{
		{
			name: "status without a systemd boot", raw: bootZH,
			setup: func(t *testing.T, f *fakeEnv) { f.systemd = false },
			run:   func(f *fakeEnv) error { return status(f.env, nil) },
			want: func(l i18n.Lang, f *fakeEnv) string {
				return languageText(l,
					"后台服务：没有 systemd 用户服务（"+bootZH+"）",
					"Background service: no systemd user service ("+bootEN+")",
					"バックグラウンドサービス：systemd ユーザーサービスがありません（"+bootJA+"）")
			},
		},
		{
			name: "install without a systemd boot", raw: bootZH,
			setup: func(t *testing.T, f *fakeEnv) { f.systemd = false },
			run:   func(f *fakeEnv) error { return install(f.env, []string{"--no-node"}) },
			want: func(l i18n.Lang, f *fakeEnv) string {
				return languageText(l,
					"没装成后台服务：这台没有可用的 systemd 用户服务（"+bootZH+"）。换一种办法让 persona-link 一直跑：",
					"No background service was installed: this computer has no usable systemd user service ("+bootEN+"). Use another way to keep persona-link running:",
					"バックグラウンドサービスをインストールできませんでした。利用可能な systemd ユーザーサービスがありません（"+bootJA+"）。別の方法で persona-link を実行し続けてください：")
			},
		},
		{
			name: "install without systemctl", raw: "没有 systemctl",
			setup: func(t *testing.T, f *fakeEnv) {
				f.lookPath = func(string) (string, error) { return "", errors.New("not found") }
			},
			run: func(f *fakeEnv) error { return install(f.env, []string{"--no-node"}) },
			want: func(l i18n.Lang, f *fakeEnv) string {
				return languageText(l,
					"没装成后台服务：这台没有可用的 systemd 用户服务（没有 systemctl）。换一种办法让 persona-link 一直跑：",
					"No background service was installed: this computer has no usable systemd user service (systemctl is unavailable). Use another way to keep persona-link running:",
					"バックグラウンドサービスをインストールできませんでした。利用可能な systemd ユーザーサービスがありません（systemctl がありません）。別の方法で persona-link を実行し続けてください：")
			},
		},
		{
			name: "uninstall without systemd", raw: bootZH, prefix: true,
			setup: func(t *testing.T, f *fakeEnv) {
				if err := os.MkdirAll(filepath.Dir(f.unitPath()), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f.unitPath(), []byte("[Unit]\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				f.systemd = false
			},
			run: func(f *fakeEnv) error { return uninstall(f.env, nil) },
			want: func(l i18n.Lang, f *fakeEnv) string {
				return languageText(l,
					"停不了服务（"+bootZH+"），只删了它的文件。\n",
					"Cannot stop the service ("+bootEN+"); removed only its files.\n",
					"サービスを停止できないため（"+bootJA+"）、ファイルのみを削除しました。\n")
			},
		},
		{
			name: "node install without systemd", raw: bootZH, exactError: true,
			setup: func(t *testing.T, f *fakeEnv) {
				if err := os.MkdirAll(filepath.Dir(f.nodeBin()), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f.nodeBin(), []byte("#!/bin/sh\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				f.systemd = false
			},
			run: func(f *fakeEnv) error { return nodeCmd(f.env, []string{"install"}) },
			want: func(l i18n.Lang, f *fakeEnv) string {
				detail := "nohup " + shellQuote(f.nodeBin()) + " node --attach >> " + shellQuote(f.nodeLog()) + " 2>&1 &"
				return languageText(l,
					"没有 systemd 用户服务（"+bootZH+"）："+detail,
					"No systemd user service ("+bootEN+"): "+detail,
					"systemd ユーザーサービスがありません（"+bootJA+"）："+detail)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, language := range terminalLanguages {
				t.Run(language.name, func(t *testing.T) {
					f := newFakeEnv(t)
					setTerminalLanguage(t, language.env)
					tc.setup(t, f)
					err := tc.run(f)
					want := tc.want(language.want, f)
					if tc.exactError {
						if got := explainLanguage(language.want, err); got != want {
							t.Fatalf("explained error = %q, want %q", got, want)
						}
						if got := err.Error(); got != tc.want(i18n.ZhHans, f) {
							t.Fatalf("canonical error = %q, want %q", got, tc.want(i18n.ZhHans, f))
						}
					} else if tc.prefix {
						if !strings.HasPrefix(f.out.String(), want) {
							t.Fatalf("output = %q, want prefix %q", f.out, want)
						}
					} else if !strings.Contains(f.out.String(), want) {
						t.Fatalf("output = %q, want text %q", f.out, want)
					}
					assertNoTerminalPlaceholder(t, commandPresentation(f, err))
					if got := f.systemdMissing(); got != tc.raw {
						t.Fatalf("canonical systemd reason = %q, want %q", got, tc.raw)
					}
				})
			}
		})
	}
}
