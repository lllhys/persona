package managercli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/clitext"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/manager"
	"github.com/nwverse/persona/server/internal/update"
)

// A flag mistake in a manager command ends with exit 2 and, on stderr, the flag's message and then the usage; a bare
// usage mistake prints the usage only, -h exits 0 with nothing more, and any other error is the caller's.
func TestManagerExit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PERSONA_HOME", filepath.Join(home, ".persona"))
	var stdout bytes.Buffer
	err := manager.Main(context.Background(), update.Build{}, []string{"capabilities", "--bogus"}, &stdout)
	var stderr bytes.Buffer
	code, done := managerExit(err, &stderr)
	if !done || code != 2 || stdout.Len() != 0 {
		t.Fatalf("capabilities --bogus: exit %d (done %v), stdout %q; want exit 2 and nothing on stdout", code, done, stdout.String())
	}
	if got := stderr.String(); !strings.HasPrefix(got, "flag provided but not defined: -bogus\n") || !strings.Contains(got, "manager join") {
		t.Fatalf("stderr %q: want the flag's message, then the usage", got)
	}

	stderr.Reset()
	if code, done := managerExit(manager.ErrUsage, &stderr); !done || code != 2 || !strings.HasPrefix(stderr.String(), "usage:\n") {
		t.Fatalf("bare usage mistake: exit %d (done %v), stderr %q; want exit 2 and the usage only", code, done, stderr.String())
	}
	stderr.Reset()
	if code, done := managerExit(manager.ErrHelp, &stderr); !done || code != 0 || stderr.Len() != 0 {
		t.Fatalf("help: exit %d (done %v), stderr %q; want exit 0 and nothing", code, done, stderr.String())
	}
	if _, done := managerExit(errors.New("boom"), &stderr); done || stderr.Len() != 0 {
		t.Fatalf("another error was handled here (stderr %q); it is the caller's", stderr.String())
	}
}

// assistant search-reindex is a command of the built-in manager's (#6340), not a usage mistake: an argument it does
// not take is its own error.
func TestAssistantSearchReindexIsACommand(t *testing.T) {
	t.Setenv("PERSONA_HOME", t.TempDir())
	err := Assistant(context.Background(), update.Build{}, []string{"search-reindex", "extra"}, nil)
	var ue clitext.UsageError
	if err == nil || errors.As(err, &ue) || !strings.Contains(err.Error(), "search-reindex takes no arguments") {
		t.Fatalf("assistant search-reindex extra: %v", err)
	}
}

func TestManagerExitSpeaksTheTerminalLanguage(t *testing.T) {
	for _, tc := range []struct {
		name, setting, language, noDatabase string
	}{
		{name: "en", setting: "en", language: "en", noDatabase: "This manager has no database. Install Docker to create its own Postgres container, or add --database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME' (the install code was not spent)"},
		{name: "zh-Hans", setting: "zh-Hans", language: "zh-Hans", noDatabase: "这个管理者没有数据库：装上 Docker（它给管理者建一个自己的 Postgres 容器），或者加 --database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'（安装码没有用掉）"},
		{name: "ja", setting: "ja", language: "ja", noDatabase: "このマネージャーにはデータベースがありません。Dockerをインストールして専用のPostgresコンテナーを作成するか、--database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'を指定してください（インストールコードは使用されていません）"},
		{name: "unknown", setting: "ko", language: "en", noDatabase: "This manager has no database. Install Docker to create its own Postgres container, or add --database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME' (the install code was not spent)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("PERSONA_HOME", filepath.Join(home, ".persona"))
			t.Setenv("PATH", t.TempDir())
			for _, name := range i18n.TerminalEnv {
				t.Setenv(name, "")
			}
			t.Setenv("PERSONA_LANG", tc.setting)

			var stdout, stderr bytes.Buffer
			err := manager.Main(context.Background(), update.Build{}, []string{"join", "http://localhost:18660", "mi_good"}, &stdout)
			code, done := managerExit(err, &stderr)
			if !done || code != 2 || stdout.Len() != 0 || stderr.String() != tc.noDatabase+"\n" {
				t.Fatalf("configuration error: exit %d (done %v), stdout %q, stderr %q", code, done, stdout.String(), stderr.String())
			}

			stderr.Reset()
			code, done = managerExit(manager.ErrUsage, &stderr)
			wantUsage := clitext.Named(manager.Usage(i18n.Lang(tc.language)))
			if !done || code != 2 || stderr.String() != wantUsage {
				t.Fatalf("usage: exit %d (done %v), stderr %q; want %q", code, done, stderr.String(), wantUsage)
			}
			stderr.Reset()
			if code, done := managerExit(manager.ErrHelp, &stderr); !done || code != 0 || stderr.Len() != 0 {
				t.Fatalf("help: exit %d (done %v), stderr %q", code, done, stderr.String())
			}
		})
	}
}
