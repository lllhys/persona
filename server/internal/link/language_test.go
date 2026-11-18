package link

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

func TestCandidateSummaryCanonicalBytesAndPresentation(t *testing.T) {
	err := errors.Join(
		&attemptError{ep: Endpoint{URL: "https://192.0.2.10:18662", Kind: "lan"}, err: newTextError("link-errors.timeout", nil)},
		&attemptError{ep: Endpoint{URL: "https://hub.example.com:18662", Kind: "name"}, err: errors.New("peer 原文：keep")},
	)
	canonical := "内网 192.0.2.10:18662：超时；域名 hub.example.com:18662：peer 原文：keep"
	if got := summary(err); got != canonical {
		t.Fatalf("summary = %q, want %q", got, canonical)
	}
	for _, language := range []i18n.Lang{i18n.En, i18n.Ja, i18n.Lang("fr")} {
		t.Setenv("PERSONA_LANG", string(language))
		if got := summary(err); got != canonical {
			t.Fatalf("language %s changed canonical summary: %q", language, got)
		}
	}
	if got := summaryText(i18n.En, err); got != "LAN 192.0.2.10:18662: timed out; Domain hub.example.com:18662: peer 原文：keep" {
		t.Fatalf("English summary = %q", got)
	}
	joined := ErrorText(i18n.En, err)
	if strings.Count(joined, "\n") != 1 || strings.Contains(joined, "；") {
		t.Fatalf("errors.Join presentation changed its newline separator: %q", joined)
	}
}

func TestStatusErrorTextRecognizesOnlyKnownShapes(t *testing.T) {
	for _, test := range []struct {
		name, canonical, english string
	}{
		{"candidate", "内网 192.0.2.10:18662：超时", "LAN 192.0.2.10:18662: timed out"},
		{"listener", ":18663：端口被占用了", ":18663: the port is already in use"},
		{"node down", "本机的常驻 node 没在运行（/tmp/node.sock 连不上）：persona-link node install（systemd 用户服务）", "This computer's resident node isn't running (cannot connect to /tmp/node.sock): run persona-link node install (systemd user service)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := StatusErrorText(i18n.En, test.canonical); got != test.english {
				t.Fatalf("got %q, want %q", got, test.english)
			}
			if got := StatusErrorText(i18n.ZhHans, test.canonical); got != test.canonical {
				t.Fatalf("canonical Chinese changed: %q", got)
			}
		})
	}
	for _, text := range []string{
		"域名 b：peer 原文：keep",
		"peer：detail",
		"/tmp/link.json: peer：detail",
		"not-an-address：端口被占用了",
		":18663：an unrecognized operating-system error",
	} {
		if got := StatusErrorText(i18n.En, text); got != text {
			t.Errorf("unknown text %q changed to %q", text, got)
		}
	}
}

func TestNodeMissingCanonicalAndLocalizedViews(t *testing.T) {
	err := nodeMissing("linux", "/tmp/node.sock")
	canonical := "本机的常驻 node 没在运行（/tmp/node.sock 连不上）：persona-link node install（systemd 用户服务）"
	if err.Error() != canonical {
		t.Fatalf("Error = %q", err)
	}
	if got := ErrorText(i18n.En, err); got != "This computer's resident node isn't running (cannot connect to /tmp/node.sock): run persona-link node install (systemd user service)" {
		t.Fatalf("English = %q", got)
	}
	if got := ErrorText(i18n.Ja, err); got == canonical || !strings.Contains(got, "/tmp/node.sock") {
		t.Fatalf("Japanese = %q", got)
	}
	if !strings.Contains(canonical, NodeDownSelf+NodeDownMarker) {
		t.Fatal("the hub and manager's canonical node-down markers changed")
	}
}

func TestMaintainerLogAndTerminalErrorViews(t *testing.T) {
	err := newTextError("link-errors.heartbeat_timeout", errors.New("boom"))
	if got, want := err.Error(), "hub 没有回应心跳：boom"; got != want {
		t.Fatalf("canonical error = %q, want %q", got, want)
	}
	if got := ErrorText(i18n.En, err); got != "The hub didn't answer the heartbeat: boom" {
		t.Fatalf("maintainer log view = %q", got)
	}
	if got := ErrorText(i18n.Ja, err); got == err.Error() || !strings.HasSuffix(got, "boom") {
		t.Fatalf("terminal view = %q", got)
	}
}

func TestPassivePeerTextDoesNotFollowTerminalLanguage(t *testing.T) {
	t.Setenv("PERSONA_LANG", "ja")
	for _, test := range []struct {
		key  string
		want string
	}{
		{"rate_limited", "认证失败太多次，稍后再试"},
		{"wrong_credential", "凭据不对：这台电脑只认配对它的那台 hub"},
		{"pair_json_required", "要一个 JSON：{code, token, machine, hub_id, hub_name}"},
		{"code_required", "没有 code"},
		{"token_invalid", "token 要 32 到 512 个可打印的 ASCII 字符，不含空白"},
		{"machine_required", "没有 machine（hub 给这台电脑的机器 id）"},
		{"bad_code", "配对码不对、过期了或者已经用过：在这台电脑上重新 persona-link pair --listen"},
		{"pair_save_failed", "存不下配对结果"},
		{"websocket_required", "要 WebSocket 升级"},
	} {
		t.Run(test.key, func(t *testing.T) {
			if got := defaultText("link-peer." + test.key); got != test.want {
				t.Fatalf("peer text = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMalformedConfigKeepsCanonicalParserDetail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link.json")
	if err := os.WriteFile(path, []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Read(path)
	if err == nil {
		t.Fatal("malformed JSON loaded")
	}
	const detail = "invalid character 'b' looking for beginning of object key string"
	canonical := path + ": " + detail
	if err.Error() != canonical {
		t.Fatalf("canonical error = %q, want %q", err, canonical)
	}
	if got := ErrorText(i18n.ZhHans, err); got != canonical {
		t.Fatalf("legacy Chinese presentation = %q, want %q", got, canonical)
	}
	if got := ErrorText(i18n.En, err); got != "Cannot read "+path+": "+detail {
		t.Fatalf("English error = %q", got)
	}
	if got := ErrorText(i18n.Ja, err); got != path+" を読み取れません："+detail {
		t.Fatalf("Japanese error = %q", got)
	}
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) || syntax.Offset != 2 {
		t.Fatalf("cause is not the JSON parser's syntax error: %T %v", syntax, err)
	}
	if !errors.Is(err, syntax) {
		t.Fatal("errors.Is does not reach the JSON parser's cause")
	}
}

func TestTextErrorCauseFillsErrorAlongsideArguments(t *testing.T) {
	cause := errors.New("disk full")
	const path = "/tmp/persona-link"
	args := i18n.Args{"path": path}
	err := newTextError("link-cli.binary_install_failed", cause, args)
	if got, want := err.Error(), "放不进 "+path+"：disk full"; got != want {
		t.Fatalf("canonical error = %q, want %q", got, want)
	}
	if got, want := ErrorText(i18n.En, err), "Cannot install "+path+": disk full"; got != want {
		t.Fatalf("English error = %q, want %q", got, want)
	}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is does not reach the install failure")
	}
	if _, mutated := args["error"]; mutated {
		t.Fatal("the constructor mutated the caller's argument map")
	}

	explicit := errors.New("shown detail")
	withExplicitError := newTextError("link-cli.binary_install_failed", cause, i18n.Args{"path": path, "error": explicit})
	if got, want := ErrorText(i18n.En, withExplicitError), "Cannot install "+path+": shown detail"; got != want {
		t.Fatalf("explicit error argument = %q, want %q", got, want)
	}
	if !errors.Is(withExplicitError, cause) {
		t.Fatal("an explicit error argument detached the wrapped cause")
	}
}
