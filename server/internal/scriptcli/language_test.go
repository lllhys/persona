package scriptcli

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/nodeclient"
)

var scriptTerminalCases = []struct {
	name, setting, language string
}{
	{name: "en", setting: "en", language: "en"},
	{name: "zh-Hans", setting: "zh-Hans", language: "zh-Hans"},
	{name: "ja", setting: "ja", language: "ja"},
	{name: "unknown falls back to en", setting: "ko", language: "en"},
}

func setScriptTerminal(t *testing.T, setting string) {
	t.Helper()
	for _, name := range i18n.TerminalEnv {
		t.Setenv(name, "")
	}
	t.Setenv("PERSONA_LANG", setting)
}

func TestAskSpeaksTheTerminalLanguage(t *testing.T) {
	tests := []struct {
		name    string
		reply   string
		dialErr error
		tty     bool
		stdin   string
		args    []string
		want    map[string]result
	}{
		{
			name: "timeout", reply: `{"status":"timeout"}`, args: []string{"Go on?"},
			want: map[string]result{
				"en":      {code: ExitTimeout, errOut: "personad ask: Timed out; nobody answered on the phone\n"},
				"zh-Hans": {code: ExitTimeout, errOut: "personad ask: 超时，手机上没人回答\n"},
				"ja":      {code: ExitTimeout, errOut: "personad ask: タイムアウトしました。スマートフォンで応答がありませんでした\n"},
			},
		},
		{
			name: "handed back without a terminal", reply: `{"status":"dismissed"}`, args: []string{"Deploy where?", "--options", "staging,prod"},
			want: map[string]result{
				"en":      {code: ExitDenied, errOut: "personad ask: The phone handed the question back to the computer\n"},
				"zh-Hans": {code: ExitDenied, errOut: "personad ask: 手机上选了「交回电脑」\n"},
				"ja":      {code: ExitDenied, errOut: "personad ask: スマートフォンで「コンピューターに戻す」が選択されました\n"},
			},
		},
		{
			name: "handed back with a terminal", reply: `{"status":"dismissed"}`, tty: true, stdin: "prod\n", args: []string{"Deploy where?", "--options", "staging,prod"},
			want: map[string]result{
				"en":      {code: ExitOK, out: "prod\n", errOut: "personad ask: The phone handed the question back to the computer; asking in the terminal\nDeploy where?\n  1) staging\n  2) prod\n> "},
				"zh-Hans": {code: ExitOK, out: "prod\n", errOut: "personad ask: 手机上选了「交回电脑」，在终端里问\nDeploy where?\n  1) staging\n  2) prod\n> "},
				"ja":      {code: ExitOK, out: "prod\n", errOut: "personad ask: スマートフォンで「コンピューターに戻す」が選択されたため、ターミナルで質問します\nDeploy where?\n  1) staging\n  2) prod\n> "},
			},
		},
		{
			name: "unreachable without a terminal", reply: `{"status":"unavailable","error":"hub detail: keep"}`, args: []string{"Go on?"},
			want: map[string]result{
				"en":      {code: ExitUnreachable, errOut: "personad ask: Cannot reach the phone: hub detail: keep\n"},
				"zh-Hans": {code: ExitUnreachable, errOut: "personad ask: 联系不上手机：hub detail: keep\n"},
				"ja":      {code: ExitUnreachable, errOut: "personad ask: スマートフォンに接続できません：hub detail: keep\n"},
			},
		},
		{
			name: "unreachable with a terminal", reply: `{"status":"unavailable","error":"hub detail: keep"}`, tty: true, stdin: "y\n", args: []string{"Go on?"},
			want: map[string]result{
				"en":      {code: ExitOK, errOut: "personad ask: Cannot reach the phone (hub detail: keep); asking in the terminal\nGo on?\n[y/N] "},
				"zh-Hans": {code: ExitOK, errOut: "personad ask: 联系不上手机（hub detail: keep），在终端里问\nGo on?\n[y/N] "},
				"ja":      {code: ExitOK, errOut: "personad ask: スマートフォンに接続できません（hub detail: keep）。ターミナルで質問します\nGo on?\n[y/N] "},
			},
		},
		{
			name: "no node", dialErr: fmt.Errorf("%w: socket detail", nodeclient.ErrNoNode), args: []string{"Go on?"},
			want: map[string]result{
				"en":      {code: ExitUnreachable, errOut: "personad ask: Cannot reach the phone: No Persona node is running (the hub is not connected to this computer)\n"},
				"zh-Hans": {code: ExitUnreachable, errOut: "personad ask: 联系不上手机：没有在运行的 Persona node（hub 没连上这台电脑）\n"},
				"ja":      {code: ExitUnreachable, errOut: "personad ask: スマートフォンに接続できません：Personaノードが実行されていません（ハブはこのコンピューターに接続していません）\n"},
			},
		},
		{
			name: "node too old", reply: `{"error":"unknown kind"}`, args: []string{"Go on?"},
			want: map[string]result{
				"en":      {code: ExitUnreachable, errOut: "personad ask: Cannot reach the phone: The personad on this computer is too old to support ask\n"},
				"zh-Hans": {code: ExitUnreachable, errOut: "personad ask: 联系不上手机：这台电脑上的 personad 太旧，不认识 ask\n"},
				"ja":      {code: ExitUnreachable, errOut: "personad ask: スマートフォンに接続できません：このコンピューターのpersonadは古いため、askを認識できません\n"},
			},
		},
	}
	for _, language := range scriptTerminalCases {
		for _, tc := range tests {
			t.Run(language.name+"/"+tc.name, func(t *testing.T) {
				setScriptTerminal(t, language.setting)
				got := ask(&fake{reply: tc.reply, err: tc.dialErr}, tc.tty, tc.stdin, tc.args...)
				if want := tc.want[language.language]; got != want {
					t.Fatalf("got %+v, want %+v", got, want)
				}
			})
		}
	}
}

func TestNotifySpeaksTheTerminalLanguage(t *testing.T) {
	tests := []struct {
		name    string
		reply   string
		dialErr error
		args    []string
		want    map[string]result
	}{
		{
			name: "no node", dialErr: fmt.Errorf("%w: socket detail", nodeclient.ErrNoNode), args: []string{"Build done"},
			want: map[string]result{
				"en":      {code: ExitOK, errOut: "personad notify: Not sent: No Persona node is running\n"},
				"zh-Hans": {code: ExitOK, errOut: "personad notify: 没发出去：没有在运行的 Persona node\n"},
				"ja":      {code: ExitOK, errOut: "personad notify: 送信できませんでした：Personaノードが実行されていません\n"},
			},
		},
		{
			name: "no node strict", dialErr: fmt.Errorf("%w: socket detail", nodeclient.ErrNoNode), args: []string{"Build done", "--strict"},
			want: map[string]result{
				"en":      {code: ExitDenied, errOut: "personad notify: Not sent: No Persona node is running\n"},
				"zh-Hans": {code: ExitDenied, errOut: "personad notify: 没发出去：没有在运行的 Persona node\n"},
				"ja":      {code: ExitDenied, errOut: "personad notify: 送信できませんでした：Personaノードが実行されていません\n"},
			},
		},
		{
			name: "queued strict", reply: `{"sent":false,"queued":true}`, args: []string{"Build done", "--strict"},
			want: map[string]result{
				"en":      {code: ExitDenied, errOut: "personad notify: Not sent: The manager is offline; the node is keeping it until the manager returns\n"},
				"zh-Hans": {code: ExitDenied, errOut: "personad notify: 没发出去：manager 不在线，node 先存着，回来后再发\n"},
				"ja":      {code: ExitDenied, errOut: "personad notify: 送信できませんでした：マネージャーがオフラインのため、ノードに保存しています。マネージャーの復帰後に送信します\n"},
			},
		},
		{
			name: "no device", reply: `{"sent":false,"reason":"no_device"}`, args: []string{"Build done"},
			want: map[string]result{
				"en":      {code: ExitOK, errOut: "personad notify: Not sent: No device can receive notifications\n"},
				"zh-Hans": {code: ExitOK, errOut: "personad notify: 没发出去：没有能接收通知的设备\n"},
				"ja":      {code: ExitOK, errOut: "personad notify: 送信できませんでした：通知を受信できるデバイスがありません\n"},
			},
		},
		{
			name: "node error passes through", reply: `{"sent":false,"error":"node detail: keep"}`, args: []string{"Build done"},
			want: map[string]result{
				"en":      {code: ExitOK, errOut: "personad notify: Not sent: node detail: keep\n"},
				"zh-Hans": {code: ExitOK, errOut: "personad notify: 没发出去：node detail: keep\n"},
				"ja":      {code: ExitOK, errOut: "personad notify: 送信できませんでした：node detail: keep\n"},
			},
		},
	}
	for _, language := range scriptTerminalCases {
		for _, tc := range tests {
			t.Run(language.name+"/"+tc.name, func(t *testing.T) {
				setScriptTerminal(t, language.setting)
				got := notify(&fake{reply: tc.reply, err: tc.dialErr}, tc.args...)
				if want := tc.want[language.language]; got != want {
					t.Fatalf("got %+v, want %+v", got, want)
				}
			})
		}
	}
}

func TestNotifyUsesTerminalInsteadOfContextLanguage(t *testing.T) {
	setScriptTerminal(t, "ja")
	f := &fake{reply: `{"sent":false,"reason":"no_device"}`}
	var out, errOut bytes.Buffer
	code := f.runner(false).Notify(i18n.With(context.Background(), i18n.En), []string{"Build done"}, &out, &errOut)
	got := result{code: code, out: out.String(), errOut: errOut.String()}
	want := result{code: ExitOK, errOut: "personad notify: 送信できませんでした：通知を受信できるデバイスがありません\n"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
