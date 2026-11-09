package node

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A path into iCloud Drive or onto a secret is never looked at: asked directly, through a link, or through a link
// in the middle of the path. Everything else resolves through its links.
func TestPathGuard(t *testing.T) {
	home := t.TempDir()
	n := &Node{home: home}
	write := func(rel, body string) {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, rel string) {
		if err := os.Symlink(target, filepath.Join(home, rel)); err != nil {
			t.Skip("symlinks:", err)
		}
	}
	write("Library/Mobile Documents/com~apple~CloudDocs/a.md", "cloud")
	write(".ssh/id_ed25519", "key")
	write(".ssh/config", "Host x")
	write(".persona/hub.json", `{"token":"x"}`)
	write(".persona/hub.json.bak-20261031", `{"token":"x"}`)
	write(".persona/apns_key.p8", "key")
	write(".persona/secret.key", "key")
	write(".persona/data/people/a.md", "# a")
	write("code/repo/id_rsa", "key")
	write("code/repo/main.go", "package main\n")
	link(filepath.Join(home, "Library/Mobile Documents"), "cloud")
	link("../.ssh", "code/keys")
	link("repo/main.go", "code/main.go")
	link(filepath.Join(home, "code/repo"), "repo")

	stat := n.statPaths([]string{
		"~/Library/Mobile Documents/com~apple~CloudDocs/a.md",
		"~/library/mobile documents/com~apple~CloudDocs/a.md", // macOS: case-insensitive
		"~/cloud/com~apple~CloudDocs/a.md",                    // a link into iCloud Drive
		"~/.ssh/config",
		"~/code/keys/id_ed25519", // a link to ~/.ssh
		"~/.persona/hub.json",
		"~/.persona/hub.json.bak-20261031",
		"~/.persona/apns_key.p8",
		"~/.persona/secret.key",
		"~/code/repo/id_rsa",
		"~/.persona/data/people/a.md",
		"~/code/main.go", // a link to a file
		"~/repo/main.go", // a link in the middle
		"~/repo/../repo/main.go",
	}, "")
	want := []string{"missing", "missing", "missing", "missing", "missing", "missing", "missing", "missing", "missing", "missing", "file", "file", "file", "file"}
	for i, s := range stat {
		if s.Kind != want[i] {
			t.Errorf("%s: %s, want %s", s.Path, s.Kind, want[i])
		}
	}

	if _, err := n.readFile("~/cloud/com~apple~CloudDocs/a.md", "", 0, false); !errors.Is(err, errICloud) {
		t.Errorf("read through a link into iCloud Drive: %v", err)
	}
	if _, err := n.readFile("keys/id_ed25519", filepath.Join(home, "code"), 0, false); !errors.Is(err, errOffLimits) {
		t.Errorf("read a key through a link: %v", err)
	}
	if _, err := n.listDir("~/code/keys", ""); !errors.Is(err, errOffLimits) {
		t.Errorf("list ~/.ssh through a link: %v", err)
	}
	if _, _, _, err := n.openRegular("~/.persona/hub.json", ""); !errors.Is(err, errOffLimits) {
		t.Errorf("download hub.json: %v", err)
	}
	fc, err := n.readFile("main.go", filepath.Join(home, "repo"), 0, false)
	if err != nil || fc.Text != "package main\n" || fc.Path != filepath.Join(home, "repo/main.go") {
		t.Fatalf("read through links: %+v %v", fc, err)
	}
	// A listing marks a link to a directory as one, except a link into iCloud Drive (not looked at).
	ls, err := n.listDir("~", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ls["entries"].([]DirEntry) {
		switch e.Name {
		case "repo":
			if !e.Dir || !e.Symlink {
				t.Errorf("repo: %+v", e)
			}
		case "cloud":
			if e.Dir {
				t.Errorf("a link into iCloud Drive was followed: %+v", e)
			}
		}
	}
}

// A home reached through a link (macOS /var -> /private/var, a Linux /home on another disk) still guards its secret
// folders: following links lands on the physical path, which is home all the same.
func TestPathGuardHomeBehindALink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "disk", "me")
	if err := os.MkdirAll(filepath.Join(real, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, ".ssh", "config"), []byte("Host x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(real, "code"), 0o700)
	home := filepath.Join(dir, "me")
	if err := os.Symlink(real, home); err != nil {
		t.Skip("symlinks:", err)
	}
	_ = os.Symlink("../.ssh", filepath.Join(real, "code", "keys"))
	n := &Node{home: home}
	if _, err := n.listDir("~/code/keys", ""); !errors.Is(err, errOffLimits) {
		t.Errorf("list ~/.ssh through a link: %v", err)
	}
	if _, err := n.readFile("~/code/keys/config", "", 0, false); !errors.Is(err, errOffLimits) {
		t.Errorf("read ~/.ssh/config through a link: %v", err)
	}
	if _, err := n.listDir("~/code", ""); err != nil {
		t.Errorf("list ~/code: %v", err)
	}
}

func TestRealPathLoop(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("b", filepath.Join(dir, "a")); err != nil {
		t.Skip("symlinks:", err)
	}
	_ = os.Symlink("a", filepath.Join(dir, "b"))
	if _, err := (&Node{}).realPath(filepath.Join(dir, "a")); err == nil || !strings.Contains(err.Error(), "too many links") {
		t.Fatalf("a loop of links: %v", err)
	}
	// A missing component ends the walk with the error looking at it gave.
	if _, err := (&Node{}).realPath(filepath.Join(dir, "nope", "x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
}

// One stat that does not answer leaves its path missing and does not hold up the others.
func TestStatPathsTimeout(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o600)
	oldTimeout, oldStat := statTimeout, statOne
	defer func() { statTimeout, statOne = oldTimeout, oldStat }()
	hang := make(chan struct{})
	defer close(hang)
	statTimeout = 100 * time.Millisecond
	statOne = func(n *Node, asked, cwd string) PathStat {
		if asked == "~/stuck" {
			<-hang
		}
		return oldStat(n, asked, cwd)
	}
	start := time.Now()
	got := (&Node{home: dir}).statPaths([]string{"~/stuck", "~/a.go"}, "")
	if took := time.Since(start); took > time.Second {
		t.Fatalf("took %s", took)
	}
	if len(got) != 2 || got[0].Kind != "missing" || got[0].Path != "~/stuck" || got[1].Kind != "file" {
		t.Fatalf("%+v", got)
	}
}

// The credentials in the data folder are refused whatever wrote them (Persona Link, the tunnel, the daemons, a manager)
// and wherever PERSONA_HOME puts the folder; the rest of it stays readable (#5247).
func TestPathGuardDataDirCredentials(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(t.TempDir(), "persona-state")
	t.Setenv("PERSONA_HOME", state)
	n := &Node{home: home}
	write := func(p, body string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	refused := []string{
		filepath.Join(home, ".persona/link.json"),
		filepath.Join(home, ".persona/link.json"),
		filepath.Join(home, ".persona/tailcat.json"),
		filepath.Join(home, ".persona/tailcat-hub.json"),
		filepath.Join(home, ".persona/daemons.json"),
		filepath.Join(home, ".persona/managers/m1/manager.json"),
		filepath.Join(home, ".persona/managers/m1/secret.key"),
		filepath.Join(state, "hub.json"),
		filepath.Join(state, "hub.json.bak-20261106"),
		filepath.Join(state, "secret.key"),
		filepath.Join(state, "link.json"),
		filepath.Join(state, "managers/m1/manager.json"),
		filepath.Join(state, "launcher-settings/work-claude-settings.json"),
		filepath.Join(home, ".persona/launcher-settings/work-env-settings.json"),
		filepath.Join(state, "relay.json"),
		filepath.Join(state, "relay.secret"),
		filepath.Join(state, "relay-tunnel.token"),
		filepath.Join(state, "edge.token"),
		filepath.Join(state, "edge.secret"),
		filepath.Join(state, "edge.password"),
		filepath.Join(state, "docker/db-password"),
		filepath.Join(state, "devices.json.migrated"),
		filepath.Join(state, "mail_accounts.json"),
		filepath.Join(state, "extensions/data/notes/token.json"),
		filepath.Join(home, ".persona/extensions/data/notes/token.json"),
		filepath.Join(state, "managers/m1/extensions/data/notes/token.json"),
		filepath.Join(home, ".claude.json"),
		filepath.Join(home, ".claude-custom-oauth.json"),
	}
	readable := []string{
		filepath.Join(home, ".persona/data/people/a.md"),
		filepath.Join(state, "data/people/a.md"),
		filepath.Join(home, "code/link.json"), // a project's file of the same name
		filepath.Join(home, "code/extensions/data/token.json"),
		filepath.Join(state, "extensions/notes/manifest.json"),
		filepath.Join(home, "persona-state-notes/hub.json"),
		filepath.Join(state, "data/proj/edge.token"), // suffix matching is top-level only, like the key pattern's
	}
	// Key backups remain secret in default, legacy, custom and manager data folders.
	for _, dir := range []string{
		filepath.Join(home, ".persona"),
		filepath.Join(home, ".persona"),
		state,
		filepath.Join(state, "managers/m1"),
	} {
		for _, name := range []string{"secret.key", "handles.key", "hub.key", "signing.p8", "private.pem", "certificate.crt", "SECRET.KEY"} {
			for _, suffix := range []string{"", ".bak", "~", "-old", ".bak.1", "~backup", "-old.bak"} {
				refused = append(refused, filepath.Join(dir, name+suffix))
			}
		}
		for _, name := range []string{"notes.keywords", "notes.pemfile", "notes.p8data", "notes.crtinfo"} {
			readable = append(readable, filepath.Join(dir, name))
		}
	}
	readable = append(readable, filepath.Join(home, "code/secret.key.bak"))
	for _, p := range append(slices.Clone(refused), readable...) {
		write(p, `{"token":"x"}`)
	}
	for _, p := range refused {
		if _, err := n.readFile(p, "", 0, false); !errors.Is(err, errOffLimits) {
			t.Errorf("read %s: %v, want it refused", p, err)
		}
	}
	for _, p := range readable {
		if _, err := n.readFile(p, "", 0, false); err != nil {
			t.Errorf("read %s: %v", p, err)
		}
	}
	if _, err := n.listDir(state, ""); err != nil {
		t.Errorf("list the data folder: %v", err)
	}
}

// With PERSONA_HOME=~ the data folder is the home folder: its credentials are refused where they are written (directly
// in it, in a manager's folder, in the folders that hold only credentials), not every project file of the same name
// or kind anywhere under the home.
func TestPathGuardDataDirIsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", "~")
	n := &Node{home: home}
	refused := []string{
		"hub.json", "hub.json.bak-20261106", "secret.key", "handles.key", "link.json", "tailcat.json", "tailcat-hub.json",
		"daemons.json", "relay.json", "relay.secret", "relay-tunnel.token", "edge.token", "edge.secret", "edge.password",
		"devices.json.migrated", "mail_accounts.json", "apns.json", "apns_key.p8",
		"managers/m1/manager.json", "managers/m1/secret.key", "managers/m1/handles.key",
		"launcher-settings/work-claude-settings.json", "tls/hub.key", "tls/hub.crt", "link-tls/link.key",
		"docker/db-password",
		".ssh/config", ".claude.json",
	}
	readable := []string{
		"code/app/certs/dev.pem", "code/app/link.json", "code/app/devices.json", "code/svc/tls.crt", "notes/apns-setup.md",
		"code/link.json", "code/launcher-settings/notes.md", "code/managers/m1/manager.json", "data/people/a.md",
		"managers/m1/data/people/a.md", "data/proj/edge.token",
	}
	for _, rel := range append(slices.Clone(refused), readable...) {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(`{"token":"x"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range refused {
		if _, err := n.readFile("~/"+rel, "", 0, false); !errors.Is(err, errOffLimits) {
			t.Errorf("read ~/%s: %v, want it refused", rel, err)
		}
	}
	for _, rel := range readable {
		if _, err := n.readFile("~/"+rel, "", 0, false); err != nil {
			t.Errorf("read ~/%s: %v", rel, err)
		}
	}
	// A link or a way round with .. does not make a credential readable.
	if err := os.Symlink(home, filepath.Join(home, "code/back")); err != nil {
		t.Skip("symlinks:", err)
	}
	for _, rel := range []string{"code/back/hub.json", "code/back/managers/m1/manager.json", "code/../link.json", "code/app/../../tls/hub.key"} {
		if _, err := n.readFile("~/"+rel, "", 0, false); !errors.Is(err, errOffLimits) {
			t.Errorf("read ~/%s: %v, want it refused", rel, err)
		}
	}
	if _, err := n.listDir("~/code/app", ""); err != nil {
		t.Errorf("list ~/code/app: %v", err)
	}
}

// In the default layout every credential the guard knows stays refused, directly, in a manager's folder, in the
// credential-only folders and through a link into the data folder; the rest of the data folder stays readable.
func TestPathGuardDataDirDefaultLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", "")
	n := &Node{home: home}
	refused := []string{
		".persona/hub.json", ".persona/hub.json.bak-20261106", ".persona/secret.key", ".persona/handles.key",
		".persona/link.json", ".persona/tailcat.json", ".persona/tailcat-hub.json", ".persona/daemons.json",
		".persona/relay.json", ".persona/relay.secret", ".persona/relay-tunnel.token",
		".persona/edge.token", ".persona/edge.secret", ".persona/edge.password",
		".persona/devices.json", ".persona/mail_accounts.json.migrated",
		".persona/apns.json", ".persona/apns_key.p8", ".persona/managers/m1/manager.json", ".persona/managers/m1/secret.key",
		".persona/launcher-settings/work-env-settings.json", ".persona/tls/hub.key", ".persona/tls/hub.crt",
		".persona/link-tls/link.key", ".persona/link-tls/link.crt", ".persona/docker/db-password",
	}
	readable := []string{".persona/data/people/a.md", ".persona/managers/m1/data/people/a.md", "code/link.json",
		"code/certs/dev.pem", ".persona/data/proj/edge.token"}
	for _, rel := range append(slices.Clone(refused), readable...) {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(`{"token":"x"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(home, ".persona"), filepath.Join(home, "code/state")); err != nil {
		t.Skip("symlinks:", err)
	}
	for _, rel := range refused {
		if _, err := n.readFile("~/"+rel, "", 0, false); !errors.Is(err, errOffLimits) {
			t.Errorf("read ~/%s: %v, want it refused", rel, err)
		}
	}
	for _, rel := range []string{"code/state/hub.json", "code/state/managers/m1/manager.json", "code/state/tls/hub.key", "code/../.persona/link.json"} {
		if _, err := n.readFile("~/"+rel, "", 0, false); !errors.Is(err, errOffLimits) {
			t.Errorf("read ~/%s: %v, want it refused", rel, err)
		}
	}
	for _, rel := range readable {
		if _, err := n.readFile("~/"+rel, "", 0, false); err != nil {
			t.Errorf("read ~/%s: %v", rel, err)
		}
	}
	if _, err := n.listDir("~/.persona", ""); err != nil {
		t.Errorf("list the data folder: %v", err)
	}
}

// A data folder PERSONA_HOME puts below a .persona or .persona folder (a second instance, ~/.persona/inst2) keeps its
// credentials refused: they are matched where they are written in it, not only one level below the .persona folder.
func TestPathGuardDataDirNestedInPersona(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", "~/.persona/inst2")
	n := &Node{home: home}
	refused := []string{
		".persona/inst2/hub.json", ".persona/inst2/secret.key", ".persona/inst2/tls/hub.key",
		".persona/inst2/managers/m1/manager.json", ".persona/inst2/launcher-settings/work-env-settings.json",
		".persona/hub.json", ".persona/tls/hub.key",
	}
	readable := []string{".persona/inst2/data/people/a.md", ".persona/inst2/managers/m1/data/people/a.md"}
	for _, rel := range append(slices.Clone(refused), readable...) {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(`{"token":"x"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range refused {
		if _, err := n.readFile("~/"+rel, "", 0, false); !errors.Is(err, errOffLimits) {
			t.Errorf("read ~/%s: %v, want it refused", rel, err)
		}
	}
	for _, rel := range readable {
		if _, err := n.readFile("~/"+rel, "", 0, false); err != nil {
			t.Errorf("read ~/%s: %v", rel, err)
		}
	}
}

// A manager's folder outside every data folder (`personad manager run --dir /srv/mgr2`, #5877): a folder that holds
// a manager's manager.json (its hub and token) has its credentials refused as a data folder's are; its other files,
// and a project's credentials-named files beside a manager.json of its own, stay readable.
func TestPathGuardManagerDirAnywhere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", filepath.Join(t.TempDir(), "persona-state"))
	t.Setenv("PERSONA_HOME", "")
	n := &Node{home: home}
	mgr := filepath.Join(t.TempDir(), "srv", "mgr2")
	project := filepath.Join(home, "code", "app")
	write := func(p, body string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(mgr, "manager.json"), `{"id":"m-1","hub":"https://hub.example.com","token":"jb_secret","bots":{}}`)
	write(filepath.Join(project, "manager.json"), `{"name":"a project's own manager.json"}`)
	refused := []string{
		filepath.Join(mgr, "manager.json"),
		filepath.Join(mgr, "manager.json.unreadable-20261106T000000Z"),
		filepath.Join(mgr, "secret.key"),
		filepath.Join(mgr, "handles.key.bak"),
		filepath.Join(mgr, "hub.json.before-manager-json-20261106T000000.000000000Z"),
		filepath.Join(mgr, "tailcat-hub.json"),
		filepath.Join(mgr, "launcher-settings", "work-claude-settings.json"),
		filepath.Join(mgr, "extensions", "data", "notes", "token.json"),
	}
	readable := []string{
		filepath.Join(mgr, "data", "people", "a.md"),
		filepath.Join(mgr, "notes.md"),
		filepath.Join(mgr, "extensions", "notes", "manifest.json"),
		filepath.Join(project, "link.json"),
		filepath.Join(project, "certs", "dev.pem"),
		filepath.Join(project, "manager.json"),
	}
	for _, p := range append(slices.Clone(refused), readable...) {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			write(p, `{"token":"x"}`)
		}
	}
	for _, p := range refused {
		if _, err := n.readFile(p, "", 0, false); !errors.Is(err, errOffLimits) {
			t.Errorf("read %s: %v, want it refused", p, err)
		}
	}
	for _, p := range readable {
		if _, err := n.readFile(p, "", 0, false); err != nil {
			t.Errorf("read %s: %v", p, err)
		}
	}
	if _, err := n.listDir(mgr, ""); err != nil {
		t.Errorf("list the manager's folder: %v", err)
	}
}
