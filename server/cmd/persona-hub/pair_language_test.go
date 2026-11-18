package main

import (
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

func TestPairHelpSpeaksTerminalLanguage(t *testing.T) {
	goldens := map[string]string{
		"en": "Usage of pair:\n" +
			"  -add\n    \tWith --machine: add the computer to hub.json if absent (outbound connection, confirmed immediately)\n" +
			"  -android\n    \tAlso print a temporary download link for the Android app\n" +
			"  -legacy\n    \tPrint a legacy QR code using the master token\n" +
			"  -machine string\n    \tPair persona-link for the computer with kind link in hub.json\n" +
			"  -name string\n    \tWith --add: name for the new computer (may be Chinese; defaults to its id)\n" +
			"  -peer\n    \tCreate a link code for another hub (docs/server/mesh.md)\n",
		"zh-Hans": "Usage of pair:\n" +
			"  -add\n    \t和 --machine 一起用：hub.json 里没有这台时先添加（电脑来连，直接是已确认的）\n" +
			"  -android\n    \t同时打印 Android 安装包的临时下载链接\n" +
			"  -legacy\n    \t使用主 token 打印旧格式二维码\n" +
			"  -machine string\n    \t给 hub.json 里这台 kind 为 link 的电脑的 persona-link 配对\n" +
			"  -name string\n    \t和 --add 一起用：新电脑的名字（可以是中文，默认同 id）\n" +
			"  -peer\n    \t生成和另一台 hub 互联的互联码（docs/server/mesh.md）\n",
		"ja": "Usage of pair:\n" +
			"  -add\n    \t--machineと併用：hub.jsonにないコンピューターを追加（ハブへ接続し、即座に確認済み）\n" +
			"  -android\n    \tAndroidアプリの一時ダウンロードリンクも出力\n" +
			"  -legacy\n    \tマスタートークンを使って旧形式のQRコードを出力\n" +
			"  -machine string\n    \thub.jsonでkindがlinkのコンピューターにpersona-linkをペアリング\n" +
			"  -name string\n    \t--addと併用：新しいコンピューターの名前（中国語も可、既定はid）\n" +
			"  -peer\n    \t別のハブと連携するための連携コードを生成（docs/server/mesh.md）\n",
	}
	tests := []struct {
		name        string
		personaLang string
		lang        string
		golden      string
	}{
		{name: "en", personaLang: "en", golden: "en"},
		{name: "zh-Hans", personaLang: "zh-Hans", golden: "zh-Hans"},
		{name: "ja", personaLang: "ja", golden: "ja"},
		{name: "unknown falls back to English", personaLang: "fr", golden: "en"},
		{name: "LANG=en_US.UTF-8", lang: "en_US.UTF-8", golden: "en"},
	}
	home := t.TempDir()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range i18n.TerminalEnv {
				t.Setenv(name, "")
			}
			t.Setenv("PERSONA_LANG", tt.personaLang)
			t.Setenv("LANG", tt.lang)
			stdout, stderr, code := runHub(t, home, "pair", "-h")
			if code != 0 || stdout != "" || stderr != goldens[tt.golden] {
				t.Fatalf("pair -h: exit %d, stdout %q\n--- stderr ---\n%s\n--- want ---\n%s", code, stdout, stderr, goldens[tt.golden])
			}
		})
	}
}
