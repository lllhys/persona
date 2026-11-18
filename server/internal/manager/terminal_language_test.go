package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/managerconf"
	"github.com/nwverse/persona/server/internal/update"
)

type managerTerminalCase struct {
	name     string
	setting  string
	language string
}

func managerTerminalCases() []managerTerminalCase {
	return []managerTerminalCase{
		{name: "en", setting: "en", language: "en"},
		{name: "zh-Hans", setting: "zh-Hans", language: "zh-Hans"},
		{name: "ja", setting: "ja", language: "ja"},
		{name: "unknown falls back to en", setting: "ko", language: "en"},
	}
}

func setManagerTerminal(t *testing.T, setting string) {
	t.Helper()
	for _, name := range i18n.TerminalEnv {
		t.Setenv(name, "")
	}
	t.Setenv("PERSONA_LANG", setting)
}

func TestManagerJoinSpeaksTheTerminalLanguage(t *testing.T) {
	for _, tc := range managerTerminalCases() {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			setManagerTerminal(t, tc.setting)
			var acceptLanguage, requestBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != bus.JoinPath {
					http.NotFound(w, r)
					return
				}
				acceptLanguage = r.Header.Get("Accept-Language")
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				requestBody = string(body)
				_ = json.NewEncoder(w).Encode(map[string]string{"id": "m-0a1b2c", "name": "Work", "token": "jb_secret", "hub_id": "h1"})
			}))
			defer srv.Close()
			dir := filepath.Join(t.TempDir(), "manager")
			database := "postgres://me@[fd00::21]:5432/mine"
			var out bytes.Buffer
			err := Main(context.Background(), update.Build{}, []string{"join", srv.URL, "mi_good", "--dir", dir, "--database-url", database}, &out)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := out.String(), managerJoinGolden(tc.language, dir); got != want {
				t.Fatalf("join output:\n%q\nwant:\n%q", got, want)
			}
			if acceptLanguage != tc.language {
				t.Fatalf("Accept-Language %q, want %q", acceptLanguage, tc.language)
			}
			if requestBody != `{"code":"mi_good"}` {
				t.Fatalf("request body %q", requestBody)
			}
			if strings.Contains(out.String(), "jb_secret") {
				t.Fatal("the joined token was printed")
			}
			c, err := assistant.ReadManagerConfig(dir)
			if err != nil {
				t.Fatal(err)
			}
			if c.ID != "m-0a1b2c" || c.Name != "Work" || c.Token != "jb_secret" || c.Hub != srv.URL || c.HubID != "h1" || c.DatabaseURL != database {
				t.Fatalf("manager.json changed: %+v", c)
			}
		})
	}
}

func TestManagerBuiltinJoinSendsTheTerminalLanguage(t *testing.T) {
	for _, tc := range managerTerminalCases() {
		t.Run(tc.name, func(t *testing.T) {
			setManagerTerminal(t, tc.setting)
			var acceptLanguage, requestBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				acceptLanguage = r.Header.Get("Accept-Language")
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				requestBody = string(body)
				_ = json.NewEncoder(w).Encode(map[string]string{"id": assistantwire.AssistantID, "name": "Assistant", "token": "jb_builtin", "hub_id": "h1"})
			}))
			defer srv.Close()
			home := t.TempDir()
			binding, err := JoinBuiltin(context.Background(), JoinOptions{Hub: srv.URL, Code: "mi_good"}, home)
			if err != nil {
				t.Fatal(err)
			}
			if acceptLanguage != tc.language {
				t.Fatalf("Accept-Language %q, want %q", acceptLanguage, tc.language)
			}
			if requestBody != `{"builtin":true,"code":"mi_good"}` {
				t.Fatalf("request body %q", requestBody)
			}
			if binding.Hub != srv.URL || binding.HubID != "h1" || binding.Token != "jb_builtin" {
				t.Fatalf("binding changed: %+v", binding)
			}
			stored, err := managerconf.ReadBinding(home)
			if err != nil || stored != binding {
				t.Fatalf("stored binding %+v, %v; want %+v", stored, err, binding)
			}
		})
	}
}

func managerJoinGolden(language, dir string) string {
	switch language {
	case "zh-Hans":
		return fmt.Sprintf("加入了 hub：这台是管理者「Work」（m-0a1b2c）。\n  目录：%s\n  数据库：mine（--database-url 给的，没有就建）\n下一步：personad manager install --dir %s（装成常驻服务），或者在前台 personad manager run --dir %s\n", dir, dir, dir)
	case "ja":
		return fmt.Sprintf("ハブに参加しました。このコンピューターはマネージャー「Work」（m-0a1b2c）です。\n  ディレクトリー：%s\n  データベース：mine（--database-urlで指定。なければ作成します）\n次の手順：personad manager install --dir %s（常駐サービスとしてインストール）、またはフォアグラウンドでpersonad manager run --dir %sを実行\n", dir, dir, dir)
	default:
		return fmt.Sprintf("Joined the hub: this computer is manager \"Work\" (m-0a1b2c).\n  Directory: %s\n  Database: mine (from --database-url; created if missing)\nNext: personad manager install --dir %s (install the service), or run personad manager run --dir %s in the foreground\n", dir, dir, dir)
	}
}

func TestManagerJoinErrorsSpeakTheTerminalLanguage(t *testing.T) {
	for _, tc := range managerTerminalCases() {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []string{"refused", "tls_required", "conflict"} {
				t.Run(kind, func(t *testing.T) {
					isolate(t)
					setManagerTerminal(t, tc.setting)
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if kind == "refused" {
							w.WriteHeader(http.StatusUnauthorized)
							return
						}
						if kind == "conflict" {
							w.WriteHeader(http.StatusConflict)
							_, _ = w.Write([]byte(`{"error":"wrong code kind"}`))
							return
						}
						w.WriteHeader(http.StatusForbidden)
						_, _ = w.Write([]byte(`{"code":"tls_required","error":"TLS required"}`))
					}))
					defer srv.Close()
					var out bytes.Buffer
					err := Main(context.Background(), update.Build{}, []string{"join", srv.URL, "mi_good", "--database-url", "postgres://me@[fd00::21]:5432/mine"}, &out)
					var ce ConfigError
					if !errors.As(err, &ce) {
						t.Fatalf("error %v (%T), want ConfigError", err, err)
					}
					if got, want := err.Error(), managerJoinErrorGolden(tc.language, kind); got != want {
						t.Fatalf("error %q, want %q", got, want)
					}
					if out.Len() != 0 {
						t.Fatalf("stdout %q, want nothing", out.String())
					}
				})
			}
		})
	}
}

func managerJoinErrorGolden(language, kind string) string {
	if kind == "conflict" {
		if language == "ja" {
			return "ハブはコードを交換しませんでしたが、コードはまだ有効です（HTTP 409）：wrong code kind"
		}
		return "the hub did not trade the code, which is still good (HTTP 409): wrong code kind"
	}
	if kind == "tls_required" {
		switch language {
		case "zh-Hans":
			return "hub 不接受经公网明文换码，这个安装码也作废了：在 App 里重新拿一个，用 hub 的 https 地址（TLS 端口）"
		case "ja":
			return "ハブは公衆インターネット上の平文によるコード交換を許可していないため、このインストールコードは無効になりました。アプリで新しいコードを取得し、ハブのhttpsアドレス（TLSポート）を使用してください"
		default:
			return "The hub does not allow plaintext code exchange over the public internet, and this install code is now void. Get a new code in the app and use the hub's https address (TLS port)"
		}
	}
	switch language {
	case "zh-Hans":
		return "hub 不认这个安装码：它不对、过期了（10 分钟）或者已经用过；在 App 里给这个助手重新拿一个"
	case "ja":
		return "ハブがこのインストールコードを受け付けませんでした。無効、期限切れ（10分）、または使用済みです。アプリでこのアシスタントの新しいコードを取得してください"
	default:
		return "The hub rejected this install code: it is invalid, expired (10 minutes), or already used. Get a new code for this assistant in the app"
	}
}

func TestManagerWithoutDatabaseSpeaksTheTerminalLanguage(t *testing.T) {
	for _, tc := range managerTerminalCases() {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			setManagerTerminal(t, tc.setting)
			t.Setenv("PATH", t.TempDir())
			var out bytes.Buffer
			err := Main(context.Background(), update.Build{}, []string{"join", "http://localhost:18660", "mi_good"}, &out)
			var ce ConfigError
			if !errors.As(err, &ce) {
				t.Fatalf("join error %v (%T), want ConfigError", err, err)
			}
			databaseError := managerNoDatabaseGolden(tc.language)
			if err.Error() != databaseError {
				t.Fatalf("join error %q, want %q", err, databaseError)
			}

			dir := filepath.Join(t.TempDir(), "manager")
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			out.Reset()
			err = Main(ctx, update.Build{}, []string{"run", "--dir", dir, "--join", "http://localhost:18660", "mi_good"}, &out)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("run error %v, want context deadline", err)
			}
			if got, want := out.String(), managerRunWaitingGolden(tc.language, dir, databaseError); got != want {
				t.Fatalf("run output %q, want %q", got, want)
			}
			if strings.Count(out.String(), "\n") != 1 {
				t.Fatalf("run printed %d lines, want one", strings.Count(out.String(), "\n"))
			}
			if _, err := os.Stat(filepath.Join(dir, assistant.ManagerFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("manager.json was written: %v", err)
			}
		})
	}
}

func managerNoDatabaseGolden(language string) string {
	switch language {
	case "zh-Hans":
		return "这个管理者没有数据库：装上 Docker（它给管理者建一个自己的 Postgres 容器），或者加 --database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'（安装码没有用掉）"
	case "ja":
		return "このマネージャーにはデータベースがありません。Dockerをインストールして専用のPostgresコンテナーを作成するか、--database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'を指定してください（インストールコードは使用されていません）"
	default:
		return "This manager has no database. Install Docker to create its own Postgres container, or add --database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME' (the install code was not spent)"
	}
}

func TestManagerDockerDownSpeaksTheTerminalLanguage(t *testing.T) {
	goldens := map[string]string{
		"en":      "Docker is installed but not running. Run sudo systemctl start docker (on Mac, open Docker Desktop or OrbStack), then retry",
		"zh-Hans": "有 Docker，但它没在运行：运行 sudo systemctl start docker（Mac 上打开 Docker Desktop 或 OrbStack），再重来",
		"ja":      "Dockerはありますが実行されていません。sudo systemctl start dockerを実行し（MacではDocker DesktopまたはOrbStackを開き）、その後再試行してください",
	}
	for _, tc := range managerTerminalCases() {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			setManagerTerminal(t, tc.setting)
			e := newEnv(io.Discard)
			e.look = func(name string) (string, error) { return "/usr/bin/" + name, nil }
			d := &fakeDocker{down: true}
			d.wire(e)
			err := e.dockerProblem(context.Background())
			if err == nil || err.Error() != goldens[tc.language] {
				t.Fatalf("docker error %q, want %q", err, goldens[tc.language])
			}
		})
	}
}

func managerRunWaitingGolden(language, dir, databaseError string) string {
	switch language {
	case "zh-Hans":
		return fmt.Sprintf("manager: 没加入 hub，同样的命令再试也不会成：%s。在等：在 App 里拿一个新安装码，再运行一次新的安装命令（没加入过的容器会用新码重建），或者运行 personad manager join <hub 地址> <安装码> --dir %s\n", databaseError, dir)
	case "ja":
		return fmt.Sprintf("manager: ハブに参加できず、同じコマンドを再実行しても成功しません：%s。待機中：アプリで新しいインストールコードを取得し、新しいインストールコマンドを再実行する（未参加のコンテナーは新しいコードで再作成されます）か、personad manager join <ハブアドレス> <インストールコード> --dir %sを実行してください\n", databaseError, dir)
	default:
		return fmt.Sprintf("manager: could not join the hub, and rerunning the same command will not help: %s. Waiting: get a new install code in the app and rerun the new install command (a container that never joined is recreated with the new code), or run personad manager join <hub address> <install code> --dir %s\n", databaseError, dir)
	}
}

func TestManagerServiceCommandsSpeakTheTerminalLanguage(t *testing.T) {
	for _, tc := range managerTerminalCases() {
		t.Run(tc.name, func(t *testing.T) {
			home := isolate(t)
			setManagerTerminal(t, tc.setting)
			const id = "m-0a1b2c"
			dir := filepath.Join(home, ".persona", "managers", id)
			if err := assistant.WriteManagerConfig(dir, assistant.ManagerConfig{ID: id, Name: "Work", Hub: "http://[fd00::20]:18660", Token: "t"}); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			e := newEnv(&out)
			e.goos, e.home, e.user = "linux", home, ""
			e.exe = func() (string, error) { return "/opt/personad", nil }
			e.look = func(name string) (string, error) { return "/usr/bin/" + name, nil }
			e.getenv = func(string) string { return "" }
			e.run = func(context.Context, string, ...string) (string, error) { return "active\n", nil }
			d := &fakeDocker{}
			d.wire(e)

			if err := cmdInstall(context.Background(), e, nil); err != nil {
				t.Fatal(err)
			}
			if got, want := out.String(), managerInstallGolden(tc.language, dir); got != want {
				t.Fatalf("install output %q, want %q", got, want)
			}
			unitPath := e.unitPath(id)
			unit, err := os.ReadFile(unitPath)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(unit), serviceUnit(id, "/opt/personad", dir); got != want {
				t.Fatalf("service file changed:\n%s\nwant:\n%s", got, want)
			}

			out.Reset()
			if err := cmdStatus(context.Background(), e, []string{"--dir", dir}); err != nil {
				t.Fatal(err)
			}
			if got, want := out.String(), managerStatusGolden(tc.language, dir); got != want {
				t.Fatalf("status output %q, want %q", got, want)
			}

			out.Reset()
			if err := cmdUninstall(context.Background(), e, []string{"--dir", dir}); err != nil {
				t.Fatal(err)
			}
			if got, want := out.String(), managerUninstallGolden(tc.language, dir); got != want {
				t.Fatalf("uninstall output %q, want %q", got, want)
			}
			if _, err := os.Stat(unitPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("service file remains: %v", err)
			}
			if !d.called("docker stop persona-mgr-m-0a1b2c-pg") || d.called("docker volume rm") {
				t.Fatalf("database behavior changed: %s", strings.Join(d.calls, "\n"))
			}
			if c, err := assistant.ReadManagerConfig(dir); err != nil || c.DatabaseContainer == "" {
				t.Fatalf("uninstall removed the database configuration: %+v, %v", c, err)
			}
		})
	}
}

func managerInstallGolden(language, dir string) string {
	switch language {
	case "zh-Hans":
		return "数据库：建好了它自己的 Postgres（Docker 容器 persona-mgr-m-0a1b2c-pg，数据卷 persona-mgr-m-0a1b2c-data）\n装好了：管理者「Work」（m-0a1b2c）是 systemd 用户服务 persona-manager-m-0a1b2c.service，已经启动。\n  日志：journalctl --user -u persona-manager-m-0a1b2c.service -f\n"
	case "ja":
		return "データベース：専用のPostgresを作成しました（Dockerコンテナーpersona-mgr-m-0a1b2c-pg、データボリュームpersona-mgr-m-0a1b2c-data）\nインストール完了：マネージャー「Work」（m-0a1b2c）をsystemdユーザーサービスpersona-manager-m-0a1b2c.serviceとして起動しました。\n  ログ：journalctl --user -u persona-manager-m-0a1b2c.service -f\n"
	default:
		return "Database: created its own Postgres (Docker container persona-mgr-m-0a1b2c-pg, data volume persona-mgr-m-0a1b2c-data)\nInstalled: manager \"Work\" (m-0a1b2c) is running as systemd user service persona-manager-m-0a1b2c.service.\n  Log: journalctl --user -u persona-manager-m-0a1b2c.service -f\n"
	}
}

func managerStatusGolden(language, dir string) string {
	switch language {
	case "zh-Hans":
		return fmt.Sprintf("管理者「Work」（m-0a1b2c）\n  目录：%s\n  hub：http://[fd00::20]:18660\n  数据库：它自己的 Postgres（Docker 容器 persona-mgr-m-0a1b2c-pg，数据卷 persona-mgr-m-0a1b2c-data），在运行\n  服务：persona-manager-m-0a1b2c.service active\n", dir)
	case "ja":
		return fmt.Sprintf("マネージャー「Work」（m-0a1b2c）\n  ディレクトリー：%s\n  ハブ：http://[fd00::20]:18660\n  データベース：専用のPostgres（Dockerコンテナーpersona-mgr-m-0a1b2c-pg、データボリュームpersona-mgr-m-0a1b2c-data）、実行中\n  サービス：persona-manager-m-0a1b2c.service active\n", dir)
	default:
		return fmt.Sprintf("Manager \"Work\" (m-0a1b2c)\n  Directory: %s\n  Hub: http://[fd00::20]:18660\n  Database: its own Postgres (Docker container persona-mgr-m-0a1b2c-pg, data volume persona-mgr-m-0a1b2c-data), running\n  Service: persona-manager-m-0a1b2c.service active\n", dir)
	}
}

func managerUninstallGolden(language, dir string) string {
	switch language {
	case "zh-Hans":
		return fmt.Sprintf("停掉并去掉了管理者 m-0a1b2c 的服务。\n数据库容器 persona-mgr-m-0a1b2c-pg 停了，数据卷 persona-mgr-m-0a1b2c-data 和里面的数据都留着：再 install 接着用；要连数据一起删掉，加 --purge。\n目录 %s 留着；在 App 里删掉这个助手，hub 才不再认它的 token。\n", dir)
	case "ja":
		return fmt.Sprintf("マネージャーm-0a1b2cのサービスを停止して削除しました。\nデータベースコンテナーpersona-mgr-m-0a1b2c-pgを停止しました。データボリュームpersona-mgr-m-0a1b2c-dataとそのデータは保持されています。再度installすると再利用できます。データも削除するには--purgeを指定してください。\nディレクトリー%sは保持されています。アプリでこのアシスタントを削除するまで、ハブはそのtokenを受け付けます。\n", dir)
	default:
		return fmt.Sprintf("Stopped and removed manager m-0a1b2c's service.\nDatabase container persona-mgr-m-0a1b2c-pg stopped. Data volume persona-mgr-m-0a1b2c-data and its data were kept: install again to reuse them, or add --purge to delete the data too.\nDirectory %s was kept. Delete this assistant in the app so the hub stops accepting its token.\n", dir)
	}
}

func TestManagerTokenRecoverySpeaksTheTerminalLanguage(t *testing.T) {
	oldPoll, oldRetry, oldProbe := revokedPoll, revokedRetry, probeToken
	t.Cleanup(func() { revokedPoll, revokedRetry, probeToken = oldPoll, oldRetry, oldProbe })
	revokedPoll = time.Millisecond
	for _, tc := range managerTerminalCases() {
		t.Run(tc.name, func(t *testing.T) {
			setManagerTerminal(t, tc.setting)
			c := &assistant.ManagerConfig{ID: "m-0a1b2c", Hub: "http://[fd00::20]:18660", Token: "jb_old"}
			revokedRetry = 0
			probeToken = func(context.Context, *assistant.ManagerConfig) error { return nil }
			var out bytes.Buffer
			if err := awaitRestartWith(context.Background(), c, func() string { return c.Token }, &out); err != nil {
				t.Fatal(err)
			}
			if got, want := out.String(), managerTokenGolden(tc.language, false); got != want {
				t.Fatalf("accepted output %q, want %q", got, want)
			}

			out.Reset()
			revokedRetry = time.Hour
			probeToken = func(context.Context, *assistant.ManagerConfig) error {
				t.Error("probed despite a renewed token")
				return bus.ErrRevoked
			}
			if err := awaitRestartWith(context.Background(), c, func() string { return "jb_new" }, &out); err != nil {
				t.Fatal(err)
			}
			if got, want := out.String(), managerTokenGolden(tc.language, true); got != want {
				t.Fatalf("renewed output %q, want %q", got, want)
			}
		})
	}
}

func managerTokenGolden(language string, renewed bool) string {
	switch language {
	case "zh-Hans":
		if renewed {
			return "manager: [m-0a1b2c] manager.json 里有了新 token，重新连 hub\n"
		}
		return "manager: [m-0a1b2c] hub 又认这个 token 了：重新连 hub\n"
	case "ja":
		if renewed {
			return "manager: [m-0a1b2c] manager.jsonに新しいtokenがあります。ハブに再接続します\n"
		}
		return "manager: [m-0a1b2c] ハブがこのtokenを再び受け付けました。ハブに再接続します\n"
	default:
		if renewed {
			return "manager: [m-0a1b2c] manager.json has a new token; reconnecting to the hub\n"
		}
		return "manager: [m-0a1b2c] the hub accepts this token again; reconnecting\n"
	}
}

func TestManagerHelpersSpeakTheTerminalLanguage(t *testing.T) {
	for _, tc := range managerTerminalCases() {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			setManagerTerminal(t, tc.setting)
			if _, err := onlyManager(i18n.Terminal()); err == nil || err.Error() != managerOnlyNoneGolden(tc.language, managersRoot()) {
				t.Fatalf("no-manager error %v", err)
			}
			for _, id := range []string{"a", "b"} {
				if err := assistant.WriteManagerConfig(filepath.Join(managersRoot(), id), assistant.ManagerConfig{ID: id, Hub: "http://[fd00::20]:18660", Token: "t"}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := onlyManager(i18n.Terminal()); err == nil || err.Error() != managerOnlyManyGolden(tc.language) {
				t.Fatalf("multiple-manager error %v", err)
			}

			c := &assistant.ManagerConfig{DatabaseURL: "postgres://me:pw@[fd00::21]:5432/mine"}
			if got, want := databaseLine(i18n.Terminal(), c, false), managerDatabaseGivenGolden(tc.language); got != want {
				t.Fatalf("given database line %q, want %q", got, want)
			}
			if got, want := databaseLine(i18n.Terminal(), &assistant.ManagerConfig{}, false), managerDatabaseNoneGolden(tc.language); got != want {
				t.Fatalf("empty database line %q, want %q", got, want)
			}

			e := newEnv(io.Discard)
			e.look = func(string) (string, error) { return "", os.ErrNotExist }
			if got, want := e.noSystemd(context.Background()), managerSystemdGolden(tc.language, "missing", ""); got != want {
				t.Fatalf("missing systemctl %q, want %q", got, want)
			}
			e.look = func(name string) (string, error) { return "/usr/bin/" + name, nil }
			e.run = func(context.Context, string, ...string) (string, error) { return "", errors.New("exit status 1") }
			if got, want := e.noSystemd(context.Background()), managerSystemdGolden(tc.language, "unreachable", ""); got != want {
				t.Fatalf("unreachable systemd %q, want %q", got, want)
			}
			e.run = func(context.Context, string, ...string) (string, error) {
				return "connection refused", errors.New("exit status 1")
			}
			if got, want := e.noSystemd(context.Background()), managerSystemdGolden(tc.language, "detail", "connection refused"); got != want {
				t.Fatalf("systemd detail %q, want %q", got, want)
			}
		})
	}
}

func managerOnlyNoneGolden(language, dir string) string {
	switch language {
	case "zh-Hans":
		return dir + " 下没有管理者：先 personad manager join，或者用 --dir 指定目录"
	case "ja":
		return dir + "にマネージャーがありません。先にpersonad manager joinを実行するか、--dirでディレクトリーを指定してください"
	default:
		return "No managers under " + dir + ": run personad manager join first, or specify a directory with --dir"
	}
}

func managerOnlyManyGolden(language string) string {
	a := filepath.Join(managersRoot(), "a")
	b := filepath.Join(managersRoot(), "b")
	switch language {
	case "zh-Hans":
		return fmt.Sprintf("这台有 2 个管理者（%s、%s）：用 --dir 指定是哪个", a, b)
	case "ja":
		return fmt.Sprintf("このコンピューターには2個のマネージャーがあります（%s、%s）。--dirで1つを指定してください", a, b)
	default:
		return fmt.Sprintf("This computer has 2 managers (%s, %s); specify one with --dir", a, b)
	}
}

func managerDatabaseGivenGolden(language string) string {
	const database = "postgres://me:xxxxx@[fd00::21]:5432/mine"
	switch language {
	case "zh-Hans":
		return "--database-url 给的（" + database + "）"
	case "ja":
		return "--database-urlで指定（" + database + "）"
	default:
		return "from --database-url (" + database + ")"
	}
}

func managerDatabaseNoneGolden(language string) string {
	switch language {
	case "zh-Hans":
		return "还没有（install 时建）"
	case "ja":
		return "未作成（install時に作成）"
	default:
		return "none yet (created by install)"
	}
}

func managerSystemdGolden(language, kind, detail string) string {
	switch language {
	case "zh-Hans":
		switch kind {
		case "missing":
			return "没有 systemctl"
		case "detail":
			return "连不上你的 systemd 用户实例：" + detail
		default:
			return "连不上你的 systemd 用户实例"
		}
	case "ja":
		switch kind {
		case "missing":
			return "systemctlがありません"
		case "detail":
			return "systemdユーザーインスタンスに接続できません：" + detail
		default:
			return "systemdユーザーインスタンスに接続できません"
		}
	default:
		switch kind {
		case "missing":
			return "systemctl is not installed"
		case "detail":
			return "cannot reach your systemd user instance: " + detail
		default:
			return "cannot reach your systemd user instance"
		}
	}
}

type managerRoundTripFunc func(*http.Request) (*http.Response, error)

func (f managerRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestManagerJoinKeepsTheNetworkErrorCause(t *testing.T) {
	isolate(t)
	setManagerTerminal(t, "en")
	sentinel := errors.New("sentinel network failure")
	client := &http.Client{Transport: managerRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, sentinel
	})}
	_, _, err := Join(context.Background(), JoinOptions{Hub: "http://localhost:18660", Code: "mi_good",
		DatabaseURL: "postgres://me@[fd00::21]:5432/mine", HTTPClient: client})
	if !errors.Is(err, sentinel) {
		t.Fatalf("network cause was lost: %v", err)
	}
	if got, want := err.Error(), "Cannot reach hub http://localhost:18660: Post \"http://localhost:18660/api/v1/bus/join\": sentinel network failure"; got != want {
		t.Fatalf("error %q, want %q", got, want)
	}
}

func TestManagerCapabilitiesJSONIsLanguageIndependent(t *testing.T) {
	const want = "{\"capabilities\":[\"config.own\",\"agent_session_archives\",\"machines.own\"]}\n"
	for _, tc := range managerTerminalCases() {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			setManagerTerminal(t, tc.setting)
			var out bytes.Buffer
			if err := Main(context.Background(), update.Build{}, []string{"capabilities", "--json"}, &out); err != nil {
				t.Fatal(err)
			}
			if out.String() != want {
				t.Fatalf("capabilities JSON %q, want exact bytes %q", out.String(), want)
			}
		})
	}
}

func TestManagerUsageSpeaksTheTerminalLanguage(t *testing.T) {
	for _, tc := range managerTerminalCases() {
		t.Run(tc.name, func(t *testing.T) {
			setManagerTerminal(t, tc.setting)
			var out bytes.Buffer
			if err := Main(context.Background(), update.Build{}, []string{"help"}, &out); err != nil {
				t.Fatal(err)
			}
			if got, want := out.String(), managerUsageGolden(tc.language); got != want {
				t.Fatalf("usage:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func managerUsageGolden(language string) string {
	switch language {
	case "zh-Hans":
		return `usage:
  personad manager maintenance prepare|observe|recovery-recipe --help
                       preserve exact-session recovery material; no exit, restore or update execution
  personad manager update check|start|status|run|confirm|rollback --plan PATH
                       inspect or activate one explicit independent manager release plan
  personad manager update inventory --plan PATH --main ID --machine ID --machine-uid UID
                       read persisted main identity and two complete local node activity snapshots; writes nothing
  personad manager join <hub 地址> <安装码> [--dir 目录] [--database-url URL] [--fingerprint 指纹] [--install]
                       用 App 里「新建助手」给的安装码换这个助手的 token，写进目录里的 manager.json
                       （默认 ~/.persona/managers/<id>）；数据库是它自己的 Postgres：install 时在 Docker 里建
                       （容器 persona-mgr-<id>-pg），或者 --database-url 给一个
  personad manager join --builtin <hub address> [<code>] [--fingerprint <fingerprint>] [--install]
                       bind the built-in manager's own service to the hub with a code from persona-hub manager-code
                       (given, or in PERSONA_MANAGER_CODE, which other users of the machine cannot read; the binding
                       goes into ~/.persona/managers/assistant/manager.json); --install: then install its service
  personad manager run [--dir 目录] [--join <hub 地址> <安装码>]
                       运行这个管理者（--join：目录里还没有 manager.json 时先换码，容器用）
  personad manager install [--dir 目录]     建好它自己的 Postgres（Docker），装成常驻服务（Linux：systemd 用户服务；Mac：launchd）
  personad manager uninstall [--dir 目录] [--purge]
                       停掉并去掉常驻服务；它的数据库容器停掉、数据卷留着（--purge：连容器和数据卷一起删掉）
  personad manager capabilities [--json]   只读列出这个程序支持的管理者能力
  personad manager status [--dir 目录]      这台电脑上的管理者和它们的服务
  personad manager search-reindex [--dir <directory>]
                       empty this manager's chat search index in its database: it reads its chats again from the
                       start
  personad manager install --builtin [--join <hub address>] | --check
                       install the built-in manager's own service (Linux: systemd user unit persona-manager-assistant;
                       Mac: launchd); --join: bind it first with the code in PERSONA_MANAGER_CODE, unless its binding
                       works (persona-hub setup does this); --check: only say whether it can be installed here
  personad manager uninstall --builtin    stop and remove that service; its binding and data stay
  personad manager status --builtin       the built-in manager's binding and service
`
	case "ja":
		return `usage:
  personad manager maintenance prepare|observe|recovery-recipe --help
                       セッションを正確に復旧するための情報を保持します。終了、復元、更新は実行しません
  personad manager update check|start|status|run|confirm|rollback --plan PATH
                       明示した独立マネージャーリリース計画を確認または実行します
  personad manager update inventory --plan PATH --main ID --machine ID --machine-uid UID
                       保存済みのメインIDと2つの完全なローカルノードアクティビティスナップショットを読み取ります。書き込みは行いません
  personad manager join <ハブアドレス> <インストールコード> [--dir ディレクトリー] [--database-url URL] [--fingerprint フィンガープリント] [--install]
                       アプリの「新規アシスタント」で発行したインストールコードをこのアシスタントのtokenと交換し、manager.jsonに保存します
                       （デフォルトは~/.persona/managers/<id>）。データベースは専用のPostgresで、install時にDocker内に作成します
                       （コンテナーpersona-mgr-<id>-pg）。--database-urlで指定することもできます
  personad manager join --builtin <hub address> [<code>] [--fingerprint <fingerprint>] [--install]
                       persona-hub manager-codeのコードで内蔵マネージャーのサービスをハブにバインドします
                       （コードを直接指定するか、他のユーザーが読めないPERSONA_MANAGER_CODEから取得。バインドは
                       ~/.persona/managers/assistant/manager.jsonに保存）。--installを指定すると続けてサービスをインストールします
  personad manager run [--dir ディレクトリー] [--join <ハブアドレス> <インストールコード>]
                       このマネージャーを実行します（--join：manager.jsonがなければ先にコードを交換。コンテナー用）
  personad manager install [--dir ディレクトリー]     専用のPostgres（Docker）を作成し、常駐サービスとしてインストールします（Linux：systemdユーザーサービス、Mac：launchd）
  personad manager uninstall [--dir ディレクトリー] [--purge]
                       常駐サービスを停止して削除します。データベースコンテナーは停止し、データボリュームは保持します（--purge：コンテナーとボリュームも削除）
  personad manager capabilities [--json]   このプログラムが対応するマネージャー機能を読み取り専用で一覧表示します
  personad manager status [--dir ディレクトリー]      このコンピューター上のマネージャーとサービスを表示します
  personad manager search-reindex [--dir <directory>]
                       このマネージャーのデータベースにあるチャット検索インデックスを空にし、最初からチャットを読み込み直します
  personad manager install --builtin [--join <hub address>] | --check
                       内蔵マネージャーのサービスをインストールします（Linux：systemdユーザーユニットpersona-manager-assistant、
                       Mac：launchd）。--join：既存のバインドが使えない場合、PERSONA_MANAGER_CODEのコードで先にバインドします
                       （persona-hub setupでも実行）。--check：インストール可能かどうかだけを確認します
  personad manager uninstall --builtin    サービスを停止して削除します。バインドとデータは保持します
  personad manager status --builtin       内蔵マネージャーのバインドとサービスを表示します
`
	default:
		return `usage:
  personad manager maintenance prepare|observe|recovery-recipe --help
                       preserve exact-session recovery material; no exit, restore or update execution
  personad manager update check|start|status|run|confirm|rollback --plan PATH
                       inspect or activate one explicit independent manager release plan
  personad manager update inventory --plan PATH --main ID --machine ID --machine-uid UID
                       read persisted main identity and two complete local node activity snapshots; writes nothing
  personad manager join <hub address> <install code> [--dir directory] [--database-url URL] [--fingerprint fingerprint] [--install]
                       trade the install code from New assistant in the app for this assistant's token and write manager.json
                       (default ~/.persona/managers/<id>); its own Postgres is created in Docker by install
                       (container persona-mgr-<id>-pg), or supplied with --database-url
  personad manager join --builtin <hub address> [<code>] [--fingerprint <fingerprint>] [--install]
                       bind the built-in manager's own service to the hub with a code from persona-hub manager-code
                       (given, or in PERSONA_MANAGER_CODE, which other users of the machine cannot read; the binding
                       goes into ~/.persona/managers/assistant/manager.json); --install: then install its service
  personad manager run [--dir directory] [--join <hub address> <install code>]
                       run this manager (--join: trade the code first when manager.json is absent; for containers)
  personad manager install [--dir directory]     create its own Postgres (Docker) and install a persistent service (Linux: systemd user service; Mac: launchd)
  personad manager uninstall [--dir directory] [--purge]
                       stop and remove the persistent service; stop its database container but keep the volume (--purge: remove both)
  personad manager capabilities [--json]   list this program's manager capabilities without changing anything
  personad manager status [--dir directory]      managers on this computer and their services
  personad manager search-reindex [--dir <directory>]
                       empty this manager's chat search index in its database: it reads its chats again from the
                       start
  personad manager install --builtin [--join <hub address>] | --check
                       install the built-in manager's own service (Linux: systemd user unit persona-manager-assistant;
                       Mac: launchd); --join: bind it first with the code in PERSONA_MANAGER_CODE, unless its binding
                       works (persona-hub setup does this); --check: only say whether it can be installed here
  personad manager uninstall --builtin    stop and remove that service; its binding and data stay
  personad manager status --builtin       the built-in manager's binding and service
`
	}
}
