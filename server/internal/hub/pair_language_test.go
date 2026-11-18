package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/nwverse/persona/server/internal/i18n"
)

type pairLanguageTest struct {
	name        string
	personaLang string
	lang        string
	golden      string
	accept      string
}

var pairLanguageTests = []pairLanguageTest{
	{name: "en", personaLang: "en", golden: "en", accept: "en"},
	{name: "zh-Hans", personaLang: "zh-Hans", golden: "zh-Hans", accept: "zh-Hans"},
	{name: "ja", personaLang: "ja", golden: "ja", accept: "ja"},
	{name: "unknown falls back to English", personaLang: "fr", golden: "en", accept: "en"},
	{name: "LANG=en_US.UTF-8", lang: "en_US.UTF-8", golden: "en", accept: "en"},
}

func setPairLanguage(t *testing.T, lang string) {
	t.Helper()
	for _, name := range i18n.TerminalEnv {
		t.Setenv(name, "")
	}
	t.Setenv("PERSONA_LANG", lang)
}

func setPairLanguageTest(t *testing.T, tt pairLanguageTest) {
	t.Helper()
	setPairLanguage(t, tt.personaLang)
	t.Setenv("LANG", tt.lang)
}

func pairTestConfig(t *testing.T, handler http.HandlerFunc) *Config {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	return &Config{Listen: ":" + port, Token: "master-secret"}
}

func TestPairOutputSpeaksTerminalLanguage(t *testing.T) {
	p := pairCode{
		URL:       "http://example.com",
		Token:     "jp_example+&",
		ExpiresAt: time.Date(2100, time.January, 2, 3, 4, 5, 0, time.UTC),
		FP:        strings.Repeat("A", 43),
	}
	pairURL := p.pairURL()
	androidURL := p.URL + "/api/v1/app/android/apk?token=jp_example%2B%26"
	legacy := &Config{PublicURL: "http://legacy.example.com:18660", Token: "a&b=+ ?"}
	legacyURL := legacy.PairURL()
	type outputGolden struct {
		normalBeforeQR string
		normalAfterQR  string
		legacyBeforeQR string
		legacyAfterQR  string
	}
	goldens := map[string]outputGolden{
		"en": {
			normalBeforeQR: "Address: http://example.com\nPairing link: " + pairURL + "\n\nScan with the Persona App to pair:\n\n",
			normalAfterQR:  "\nValid until: 2100-01-02T03:04:05Z\nPublic-key fingerprint: " + p.FP + "\nAndroid app: " + androidURL + "\n",
			legacyBeforeQR: "Server: http://legacy.example.com:18660\nToken:  a&b=+ ?\n\nScan with the Persona App to pair:\n\n",
			legacyAfterQR:  "\nCopy this link on a Mac and paste it under “Connect hub” in Persona:\n" + legacyURL + "\n",
		},
		"zh-Hans": {
			normalBeforeQR: "地址：http://example.com\n配对链接：" + pairURL + "\n\n用 Persona App 扫码配对：\n\n",
			normalAfterQR:  "\n有效期至：2100-01-02T03:04:05Z\n公钥指纹：" + p.FP + "\nAndroid 安装包：" + androidURL + "\n",
			legacyBeforeQR: "Server: http://legacy.example.com:18660\nToken:  a&b=+ ?\n\n用 Persona App 扫码配对：\n\n",
			legacyAfterQR:  "\nMac 上拷贝这条链接，在 Persona 的「连接 hub」里粘贴：\n" + legacyURL + "\n",
		},
		"ja": {
			normalBeforeQR: "アドレス：http://example.com\nペアリングリンク：" + pairURL + "\n\nPersonaアプリでQRコードをスキャンしてペアリング：\n\n",
			normalAfterQR:  "\n有効期限：2100-01-02T03:04:05Z\n公開鍵フィンガープリント：" + p.FP + "\nAndroidアプリ：" + androidURL + "\n",
			legacyBeforeQR: "サーバー：http://legacy.example.com:18660\nトークン：a&b=+ ?\n\nPersonaアプリでQRコードをスキャンしてペアリング：\n\n",
			legacyAfterQR:  "\nMacでこのリンクをコピーし、Personaの「ハブに接続」に貼り付けてください：\n" + legacyURL + "\n",
		},
	}
	for _, tt := range pairLanguageTests {
		t.Run(tt.name, func(t *testing.T) {
			setPairLanguageTest(t, tt)
			accept := make(chan string, 1)
			cfg := pairTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
				accept <- r.Header.Get("Accept-Language")
				_ = json.NewEncoder(w).Encode(p)
			})
			cfg.PublicURL = p.URL + "/"
			var out bytes.Buffer
			if err := cfg.printPair(context.Background(), false, &out, true); err != nil {
				t.Fatal(err)
			}
			if got := <-accept; got != tt.accept {
				t.Fatalf("Accept-Language = %q, want %q", got, tt.accept)
			}
			var qr bytes.Buffer
			qrterminal.GenerateHalfBlock(pairURL, qrterminal.L, &qr)
			golden := goldens[tt.golden]
			if want := golden.normalBeforeQR + qr.String() + golden.normalAfterQR; out.String() != want {
				t.Fatalf("normal pair output changed\n--- got ---\n%s\n--- want ---\n%s", out.String(), want)
			}

			out.Reset()
			writePair(&out, legacy)
			qr.Reset()
			qrterminal.GenerateHalfBlock(legacyURL, qrterminal.L, &qr)
			if want := golden.legacyBeforeQR + qr.String() + golden.legacyAfterQR; out.String() != want {
				t.Fatalf("legacy pair output changed\n--- got ---\n%s\n--- want ---\n%s", out.String(), want)
			}
		})
	}
}

func TestPairMachineOutputSpeaksTerminalLanguage(t *testing.T) {
	expires := time.Now().Add(pairLifetime + 15*time.Second)
	p := pairCode{Token: "jp_machine", ExpiresAt: expires, URL: "https://hub.example.com", Machine: "study"}
	wantOut := "persona://link?machine=study&token=jp_machine&url=https%3A%2F%2Fhub.example.com\n"
	helpGoldens := map[string]string{
		"en": "\nAdded: 书房\n" +
			"\nThis one-time Persona Link pairing link is for “书房” and is valid for 10 minutes.\n" +
			"On that computer, paste it under Persona Link › Pair / Change Connection… in the menu bar, or run persona-link pair '<link>' (a new installation also uses persona-link).\n",
		"zh-Hans": "\n已添加：书房\n" +
			"\n这是给「书房」的 Persona Link 用的配对链接，10 分钟内有效，只能用一次。\n" +
			"在那台电脑上：菜单栏的 Persona Link › 配对 / 修改连接… 里粘贴；或者运行 persona-link pair '<链接>'（新装的也可以用 persona-link）。\n",
		"ja": "\n追加しました：书房\n" +
			"\nこれは「书房」用のPersona Linkペアリングリンクです。10分間有効で、1回だけ使えます。\n" +
			"そのコンピューターで、メニューバーのPersona Link › ペアリング / 接続を変更…に貼り付けるか、persona-link pair '<リンク>'を実行してください（新規インストールでもpersona-linkを使えます）。\n",
	}
	for _, tt := range pairLanguageTests {
		t.Run(tt.name, func(t *testing.T) {
			setPairLanguageTest(t, tt)
			accept := make(chan string, 1)
			cfg := pairTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
				accept <- r.Header.Get("Accept-Language")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"token": p.Token, "expires_at": p.ExpiresAt, "url": p.URL, "machine": p.Machine,
					"name": "书房", "trust": "confirmed", "added": true,
				})
			})
			var out, help bytes.Buffer
			if err := cfg.printLinkPair(context.Background(), "study", true, "书房", &out, &help); err != nil {
				t.Fatal(err)
			}
			if got := <-accept; got != tt.accept {
				t.Fatalf("Accept-Language = %q, want %q", got, tt.accept)
			}
			if out.String() != wantOut {
				t.Fatalf("machine stdout = %q, want unchanged bytes %q", out.String(), wantOut)
			}
			if want := helpGoldens[tt.golden]; help.String() != want {
				t.Fatalf("machine help = %q, want %q", help.String(), want)
			}
		})
	}
}

func TestPairErrorsSpeakTerminalLanguage(t *testing.T) {
	type errorGolden struct {
		conflict       string
		listen         string
		unreachable    string
		temporaryToken string
		invalidReply   string
		noPublicURL    string
		refusal        string
		unknownMachine string
		wrongMachine   string
	}
	goldens := map[string]errorGolden{
		"en": {
			conflict:       "--android needs a temporary token and cannot be combined with --legacy",
			listen:         "The hub’s listen configuration has no valid port. Check the configuration, or use --legacy to print a legacy QR code",
			unreachable:    "Can’t connect to the hub on this machine. It may not be running, or hub.json’s listen names an address this machine doesn’t have. Start the hub first, or use --legacy to print a legacy QR code",
			temporaryToken: "This hub failed to create a temporary token (HTTP 502). Check that the hub is upgraded and its configuration matches, or use --legacy to print a legacy QR code",
			invalidReply:   "This hub returned invalid pairing information. Upgrade the hub, or use --legacy to print a legacy QR code",
			noPublicURL:    "The hub does not know which address phones should use to reach it (public_url is empty). With Docker, set PERSONA_PUBLIC_URL=http://<this machine's LAN address>:18660 in .env and run docker compose up -d. Without Docker, set public_url in hub.json and restart the hub. You can also set the address under Network in the admin page",
			refusal:        "This hub did not create a pairing link (HTTP 403): raw hub detail",
			unknownMachine: "hub.json has no computer “study” for an outbound connection. Add it automatically with --add: personad hub pair --machine study --add [--name <name>]",
			wrongMachine:   "This hub did not create a pairing link for computer “study”. It may not have been upgraded or restarted yet. Upgrade and restart the hub, then try again",
		},
		"zh-Hans": {
			conflict:       "--android 需要临时 token，不能与 --legacy 同时使用",
			listen:         "hub listen 配置没有有效端口；请检查配置，或用 --legacy 打印旧格式二维码",
			unreachable:    "无法连接本机 hub：hub 可能未运行，或 hub.json 的 listen 写的不是本机的地址；请先启动 hub，或用 --legacy 打印旧格式二维码",
			temporaryToken: "本机 hub 生成临时 token 失败（HTTP 502）；请确认 hub 已升级且配置一致，或用 --legacy 打印旧格式二维码",
			invalidReply:   "本机 hub 返回了无效的配对信息；请升级 hub，或用 --legacy 打印旧格式二维码",
			noPublicURL:    "hub 还不知道手机从哪个地址连它（public_url 是空的）：Docker 里在 .env 写上 PERSONA_PUBLIC_URL=http://<这台机器的局域网地址>:18660，再运行 docker compose up -d；不用 Docker 的，在 hub.json 里填 public_url 后重启 hub。也可以在管理界面的「网络」里填地址",
			refusal:        "本机 hub 没有生成配对链接（HTTP 403）：raw hub detail",
			unknownMachine: "hub.json 里没有要「电脑来连」的电脑「study」；加 --add 自动添加：personad hub pair --machine study --add [--name <名字>]",
			wrongMachine:   "本机 hub 没有生成电脑「study」的配对链接：hub 可能还没升级或没重启；升级并重启 hub 后再试",
		},
		"ja": {
			conflict:       "--androidには一時トークンが必要なため、--legacyと同時には使えません",
			listen:         "ハブのlisten設定に有効なポートがありません。設定を確認するか、--legacyで旧形式のQRコードを出力してください",
			unreachable:    "このマシンのハブに接続できません。ハブが実行されていないか、hub.json の listen がこのマシンにないアドレスになっている可能性があります。ハブを起動するか、--legacyで旧形式のQRコードを出力してください",
			temporaryToken: "このハブは一時トークンを作成できませんでした（HTTP 502）。ハブの更新と設定を確認するか、--legacyで旧形式のQRコードを出力してください",
			invalidReply:   "このハブから無効なペアリング情報が返されました。ハブを更新するか、--legacyで旧形式のQRコードを出力してください",
			noPublicURL:    "ハブはスマートフォンからの接続先アドレスをまだ認識していません（public_urlが空です）。Dockerでは.envにPERSONA_PUBLIC_URL=http://<このマシンのLANアドレス>:18660を設定し、docker compose up -dを実行してください。Docker以外ではhub.jsonにpublic_urlを設定してハブを再起動してください。管理画面の「ネットワーク」でも設定できます",
			refusal:        "このハブはペアリングリンクを生成できませんでした（HTTP 403）：raw hub detail",
			unknownMachine: "hub.jsonにハブへ接続するコンピューター「study」がありません。--addを付けると自動で追加します：personad hub pair --machine study --add [--name <名前>]",
			wrongMachine:   "このハブはコンピューター「study」のペアリングリンクを生成しませんでした。ハブの更新または再起動が必要な可能性があります。ハブを更新して再起動してから再試行してください",
		},
	}
	check := func(t *testing.T, err error, want string) {
		t.Helper()
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
	for _, tt := range pairLanguageTests {
		t.Run(tt.name, func(t *testing.T) {
			setPairLanguageTest(t, tt)
			want := goldens[tt.golden]

			t.Run("legacy Android conflict", func(t *testing.T) {
				var out bytes.Buffer
				err := (&Config{}).printPair(context.Background(), true, &out, true)
				check(t, err, want.conflict)
				if out.Len() != 0 {
					t.Fatalf("conflict wrote stdout %q", out.String())
				}
			})
			t.Run("invalid listen", func(t *testing.T) {
				_, err := (&Config{Listen: "invalid"}).requestPairCode(context.Background())
				check(t, err, want.listen)
			})
			t.Run("unreachable", func(t *testing.T) {
				srv := httptest.NewServer(http.NotFoundHandler())
				_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
				srv.Close()
				_, err := (&Config{Listen: ":" + port}).requestPairCode(context.Background())
				check(t, err, want.unreachable)
			})
			t.Run("temporary token refused", func(t *testing.T) {
				cfg := pairTestConfig(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
				_, err := cfg.requestPairCode(context.Background())
				check(t, err, want.temporaryToken)
			})
			t.Run("invalid response", func(t *testing.T) {
				cfg := pairTestConfig(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
				_, err := cfg.requestPairCode(context.Background())
				check(t, err, want.invalidReply)
			})
			t.Run("missing public URL", func(t *testing.T) {
				cfg := pairTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
					_ = json.NewEncoder(w).Encode(pairCode{Token: "jp_temporary", ExpiresAt: time.Now().Add(pairLifetime)})
				})
				_, err := cfg.requestPairCode(context.Background())
				check(t, err, want.noPublicURL)
			})
			t.Run("structured refusal", func(t *testing.T) {
				cfg := pairTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"error":"raw hub detail","code":"refused"}`))
				})
				_, err := cfg.requestPairCodeWith(context.Background(), map[string]any{"machine": "study"})
				check(t, err, want.refusal)
				var refusal *pairRefusal
				if !errors.As(err, &refusal) || refusal.code != "refused" {
					t.Fatalf("structured refusal changed: %#v", err)
				}
			})
			t.Run("unknown machine", func(t *testing.T) {
				cfg := pairTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"raw hub detail","code":"unknown_machine"}`))
				})
				var out, help bytes.Buffer
				err := cfg.printLinkPair(context.Background(), "study", false, "", &out, &help)
				check(t, err, want.unknownMachine)
				if out.Len() != 0 || help.Len() != 0 {
					t.Fatalf("unknown machine wrote stdout %q or stderr %q", out.String(), help.String())
				}
			})
			t.Run("old hub returned an app code", func(t *testing.T) {
				cfg := pairTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
					_ = json.NewEncoder(w).Encode(pairCode{Token: "jp_app", ExpiresAt: time.Now().Add(pairLifetime), URL: "https://hub.example.com"})
				})
				var out, help bytes.Buffer
				err := cfg.printLinkPair(context.Background(), "study", true, "书房", &out, &help)
				check(t, err, want.wrongMachine)
				if out.Len() != 0 || help.Len() != 0 {
					t.Fatalf("old hub wrote stdout %q or stderr %q", out.String(), help.String())
				}
			})
		})
	}
}

func TestSetupPairCodeUsesSetupLanguage(t *testing.T) {
	setPairLanguage(t, "en")
	var accept string
	cfg := pairTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept-Language")
		w.WriteHeader(http.StatusBadGateway)
	})
	var out bytes.Buffer
	e := &setupEnv{l: i18n.Ja, o: setupOptions{json: true}, out: &out}
	err := e.finish(context.Background(), &setupConfig{Config: cfg})
	if !errors.Is(err, ErrSetupStopped) {
		t.Fatalf("finish error = %v, want ErrSetupStopped", err)
	}
	if accept != "ja" {
		t.Errorf("Accept-Language = %q, want setup's explicit ja", accept)
	}
	var report struct {
		Step   string `json:"step"`
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
		Next   string `json:"next"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &report); err != nil {
		t.Fatalf("setup report %q: %v", out.String(), err)
	}
	const wantDetail = "ハブは実行中ですが、ペアリングコードを取得できませんでした：このハブは一時トークンを作成できませんでした（HTTP 502）。ハブの更新と設定を確認するか、--legacyで旧形式のQRコードを出力してください"
	if report.Step != "health" || report.OK || report.Detail != wantDetail || report.Next != "personad hub pair を実行してもう一度試してください" {
		t.Fatalf("setup report = %+v, want Japanese pair failure", report)
	}
}
