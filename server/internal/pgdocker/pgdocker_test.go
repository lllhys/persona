package pgdocker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestURLPreservesPassword(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	t.Setenv("PGSERVICEFILE", "")
	for _, tc := range []struct{ name, password string }{
		{"space", "has space"},
		{"plus", "literal+plus"},
		{"reserved", "colon:at@slash/question?hash#percent%"},
		{"generated", strings.Repeat("ab", 24)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := pgx.ParseConfig(URL(tc.password, 25432))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Password != tc.password {
				t.Errorf("password = %q, want %q", cfg.Password, tc.password)
			}
			if cfg.User != User || cfg.Host != "127.0.0.1" || cfg.Port != 25432 || cfg.Database != Database || cfg.TLSConfig != nil {
				t.Error("database URL changed its connection settings")
			}
		})
	}
	retained := &Container{User: "retained_role", Database: "retained_db"}
	cfg, err := pgx.ParseConfig(retained.URL("has space", 25432))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "has space" || cfg.User != retained.User || cfg.Database != retained.Database {
		t.Error("retained container credentials changed in its URL")
	}
}

// fake is docker on a machine with no containers yet: run makes one (with the password it was given), rm removes it.
type fake struct {
	calls     []string
	password  string
	user      string // the POSTGRES_USER (and POSTGRES_DB) it was made with
	exists    bool
	port      int  // the port the container publishes (docker port); 0: none
	taken     int  // a port someone else holds now: docker start fails while the container publishes it
	published bool // the container is published at all (false: made on a network of its own)
}

func (f *fake) run(_ context.Context, env []string, name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, call)
	switch {
	case strings.HasPrefix(call, "docker ps -a") && f.exists:
		return []byte(ManagerContainer("m-0a1b2c") + "\n"), nil
	case strings.HasPrefix(call, "docker run"):
		f.exists, f.published = true, true
		for i, a := range args {
			if a == "-p" {
				_, _ = fmt.Sscanf(args[i+1], "127.0.0.1:%d:5432", &f.port)
			}
		}
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, "POSTGRES_PASSWORD="); ok {
				f.password = v
			}
		}
		for _, a := range args {
			if v, ok := strings.CutPrefix(a, "POSTGRES_USER="); ok {
				f.user = v
			}
		}
	case strings.HasPrefix(call, "docker inspect -f {{range .Config.Env}}"):
		return []byte("POSTGRES_USER=" + f.user + "\nPOSTGRES_PASSWORD=" + f.password + "\nPOSTGRES_DB=" + f.user + "\n"), nil
	case strings.HasPrefix(call, "docker port"):
		if !f.published {
			return nil, errors.New("exit status 1")
		}
		return []byte(fmt.Sprintf("127.0.0.1:%d\n", f.port)), nil
	case strings.HasPrefix(call, "docker start") && f.taken != 0 && f.port == f.taken:
		return []byte("Error response from daemon: driver failed programming external connectivity: Bind for 127.0.0.1:25432 failed: port is already allocated"), errors.New("exit status 1")
	case strings.HasPrefix(call, "docker rm -f"):
		f.exists = false
	}
	return nil, nil
}

func (f *fake) count(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// The first call makes the container with a new password, on 127.0.0.1, and returns its URL; later calls (with the
// kept password, or none: read back from the container) make nothing and return the same URL. It writes no file.
func TestProvisionManagerDBIsIdempotent(t *testing.T) {
	f := &fake{}
	o := ManagerDBOptions{Run: f.run, LookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
		FreePort: func() (int, error) { return 25432, nil }, Sleep: func(time.Duration) {}, Wait: time.Second}
	ctx := context.Background()
	url, created, err := ProvisionManagerDB(ctx, "m-0a1b2c", o)
	if err != nil || !created || len(f.password) != 48 || url != URL(f.password, 25432) {
		t.Fatalf("first: %q %v %v", url, created, err)
	}
	if f.count("docker volume create persona-mgr-m-0a1b2c-data") != 1 ||
		f.count("docker run -d --name persona-mgr-m-0a1b2c-pg --restart unless-stopped -p 127.0.0.1:25432:5432") != 1 {
		t.Fatalf("calls: %s", strings.Join(f.calls, "\n"))
	}
	for _, c := range f.calls {
		if strings.Contains(c, f.password) {
			t.Fatalf("the password on a command line: %s", c)
		}
	}
	for _, kept := range []string{f.password, ""} {
		o.Password = kept
		again, created, err := ProvisionManagerDB(ctx, "m-0a1b2c", o)
		if err != nil || created || again != url || f.count("docker run") != 1 {
			t.Fatalf("again (kept %q): %q %v %v", kept, again, created, err)
		}
	}
	f2 := &fake{}
	o.Run, o.LookPath = f2.run, func(string) (string, error) { return "", errors.New("none") }
	if _, _, err := ProvisionManagerDB(ctx, "m-0a1b2c", o); !errors.Is(err, ErrNoDocker) || len(f2.calls) != 0 {
		t.Fatalf("no docker: %v %v", err, f2.calls)
	}
}

// A container is made running postgres with ServerArgs after the image (lz4 for the WAL's full page images, fewer
// checkpoints), and every docker run of that image in scripts/deploy/db.sh passes the same arguments. A continuation,
// a redirect or the if/then around the run is shell, not a server argument.
func TestCreateRunsPostgresWithServerArgs(t *testing.T) {
	f := &fake{}
	c := &Container{Name: "persona-postgres", Volume: "persona-pgdata", Port: 18661, Run: f.run}
	if err := c.Create(context.Background(), "pw"); err != nil {
		t.Fatal(err)
	}
	want := " " + Image + " postgres -c wal_compression=lz4 -c checkpoint_timeout=15min"
	if f.count("docker run") != 1 || !strings.HasSuffix(f.calls[len(f.calls)-1], want) {
		t.Fatalf("docker run does not end with %q:\n%s", want, strings.Join(f.calls, "\n"))
	}
	dbSh, err := os.ReadFile("../../../scripts/deploy/db.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := dbShServerArgsMismatch(string(dbSh)); err != nil {
		t.Error(err)
	}
}

// The guard matches ServerArgs exactly. Another line ending or conditional still passes; a missing, different, extra
// or reordered argument does not, and a comment that only mentions the image is not a docker run.
func TestDBShServerArgsGuardIsExact(t *testing.T) {
	args := Image + " " + strings.Join(ServerArgs, " ")
	for _, tc := range []struct {
		name    string
		script  string
		wantErr bool
	}{
		{"conditional", "if ! docker run -d \\\n    " + args + " >/dev/null; then\n  exit 1\nfi\n", false},
		{"redirect only", "docker run " + args + " >/dev/null\n", false},
		{"crlf", "docker run " + args + " >/dev/null\r\n", false},
		{"two runs", "docker run " + args + "\ndocker run " + args + " >/dev/null; then\n", false},
		{"missing", "docker run " + Image + " postgres -c wal_compression=lz4 >/dev/null\n", true},
		{"different", "docker run " + Image + " postgres -c wal_compression=off -c checkpoint_timeout=15min\n", true},
		{"extra", "docker run " + args + " -c max_connections=10 >/dev/null\n", true},
		{"reordered", "docker run " + Image + " postgres -c checkpoint_timeout=15min -c wal_compression=lz4\n", true},
		{"second run differs", "docker run " + args + "\ndocker run " + Image + " postgres\n", true},
		{"comment only", "# docker run " + args + "\n", true},
		{"other image", "docker run postgres:16\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := dbShServerArgsMismatch(tc.script)
			if tc.wantErr && err == nil {
				t.Fatal("expected a mismatch")
			}
			if !tc.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

// dbShServerArgsMismatch reports when script does not pass exactly ServerArgs after Image on every docker run of that
// image. No such run is a mismatch. Backslash continuations are joined first; redirects and other shell terminators
// (;, &&, ||, |, &) are not arguments.
func dbShServerArgsMismatch(script string) error {
	var runs [][]string
	for _, line := range logicalShellLines(script) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		rest := line
		for strings.TrimSpace(rest) != "" {
			args, next := splitSimpleCommand(rest)
			if next == rest {
				break
			}
			for i := 0; i+1 < len(args); i++ {
				if args[i] != "docker" || args[i+1] != "run" {
					continue
				}
				for j := i + 2; j < len(args); j++ {
					if args[j] == Image {
						runs = append(runs, slices.Clone(args[j+1:]))
						break
					}
				}
				break
			}
			rest = next
		}
	}
	if len(runs) == 0 {
		return fmt.Errorf("scripts/deploy/db.sh does not start %s with %v", Image, ServerArgs)
	}
	for _, args := range runs {
		if !slices.Equal(args, ServerArgs) {
			return fmt.Errorf("scripts/deploy/db.sh runs %s with %q, want %q", Image, args, ServerArgs)
		}
	}
	return nil
}

// logicalShellLines joins a trailing backslash with the next line and turns CR LF into LF.
func logicalShellLines(script string) []string {
	script = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(script)
	raw := strings.Split(script, "\n")
	lines := make([]string, 0, len(raw))
	var b strings.Builder
	for _, line := range raw {
		if strings.HasSuffix(line, "\\") {
			b.WriteString(line[:len(line)-1])
			b.WriteByte(' ')
			continue
		}
		b.WriteString(line)
		lines = append(lines, b.String())
		b.Reset()
	}
	if b.Len() > 0 {
		lines = append(lines, b.String())
	}
	return lines
}

// splitSimpleCommand returns the words of the first simple command in s, without its redirects, and the text after
// the terminator that ended it. Quotes are shell syntax: the word is what the program receives.
func splitSimpleCommand(s string) (args []string, rest string) {
	var b strings.Builder
	inWord := false
	var quote byte
	flush := func() {
		if inWord {
			args = append(args, b.String())
			b.Reset()
			inWord = false
		}
	}
	skipTarget := func(i int) int {
		if i >= len(s) {
			return i
		}
		if s[i] == '\'' || s[i] == '"' {
			q := s[i]
			i++
			for i < len(s) && s[i] != q {
				i++
			}
			if i < len(s) {
				i++
			}
			return i
		}
		for i < len(s) && s[i] != ' ' && s[i] != '\t' && !isShellMeta(s[i]) {
			i++
		}
		return i
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			} else {
				b.WriteByte(c)
			}
			inWord = true
			continue
		}
		if c == '\\' && i+1 < len(s) {
			i++
			b.WriteByte(s[i])
			inWord = true
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			inWord = true
			continue
		}
		if c == ' ' || c == '\t' {
			flush()
			continue
		}
		if c == '#' {
			flush()
			return args, ""
		}
		if c == ';' {
			flush()
			return args, s[i+1:]
		}
		if c == '&' && i+1 < len(s) && s[i+1] == '>' {
			flush()
			i += 2
			for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
			i = skipTarget(i) - 1
			continue
		}
		if c == '&' || c == '|' {
			flush()
			if i+1 < len(s) && s[i+1] == c {
				return args, s[i+2:]
			}
			return args, s[i+1:]
		}
		if c == '>' || c == '<' {
			flush()
			i++
			if i < len(s) && (s[i] == '>' || s[i] == '&' || s[i] == '|') {
				i++
			}
			for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
			i = skipTarget(i) - 1
			continue
		}
		if c >= '0' && c <= '9' && !inWord {
			j := i
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			if j < len(s) && (s[j] == '>' || s[j] == '<') {
				i = j - 1
				continue
			}
		}
		b.WriteByte(c)
		inWord = true
	}
	flush()
	return args, ""
}

func isShellMeta(c byte) bool {
	switch c {
	case ';', '|', '&', '>', '<', '#':
		return true
	default:
		return false
	}
}

// A stopped container whose port someone took meanwhile is made again on a new free port, over the same volume and
// with the same password, and the new URL comes back for the caller to keep.
func TestProvisionManagerDBMovesOffATakenPort(t *testing.T) {
	f := &fake{}
	ports := []int{25432, 25433}
	o := ManagerDBOptions{Run: f.run, LookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
		FreePort: func() (int, error) { p := ports[0]; ports = ports[1:]; return p, nil }, Sleep: func(time.Duration) {}, Wait: time.Second}
	ctx := context.Background()
	first, _, err := ProvisionManagerDB(ctx, "m-0a1b2c", o)
	if err != nil {
		t.Fatal(err)
	}
	f.taken = 25432
	o.Password = f.password
	moved, created, err := ProvisionManagerDB(ctx, "m-0a1b2c", o)
	if err != nil || created || moved == first || moved != URL(o.Password, 25433) || f.count("docker volume rm") != 0 ||
		f.count("docker rm -f persona-mgr-m-0a1b2c-pg") != 1 || f.count("docker volume create persona-mgr-m-0a1b2c-data") != 2 {
		t.Fatalf("moved: %q %v %v\n%s", moved, created, err, strings.Join(f.calls, "\n"))
	}
}

// A container by its name that publishes no port (made on a network of its own by the docker script) can't be used
// from this machine: a clear error, not a URL with port 0.
func TestProvisionManagerDBNotPublished(t *testing.T) {
	f := &fake{exists: true, password: "pw"}
	o := ManagerDBOptions{Run: f.run, LookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
		FreePort: func() (int, error) { return 25432, nil }, Sleep: func(time.Duration) {}, Wait: time.Second}
	if url, _, err := ProvisionManagerDB(context.Background(), "m-0a1b2c", o); !errors.Is(err, ErrNotPublished) || url != "" ||
		!strings.Contains(err.Error(), "docker rm -f persona-mgr-m-0a1b2c-pg") {
		t.Fatalf("not published: %q %v", url, err)
	}
}

// A manager's container made before the rename has the role and database jarvis (#6149): its URL keeps them, also when
// it is made again on another port, instead of today's persona, which it has no role for.
func TestProvisionManagerDBKeepsAnOlderContainersOwnUser(t *testing.T) {
	f := &fake{exists: true, published: true, port: 25432, user: "jarvis", password: "kept"}
	ports := []int{25433}
	o := ManagerDBOptions{Run: f.run, LookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
		FreePort: func() (int, error) { p := ports[0]; ports = ports[1:]; return p, nil }, Sleep: func(time.Duration) {}, Wait: time.Second}
	ctx := context.Background()
	got, created, err := ProvisionManagerDB(ctx, "m-0a1b2c", o)
	want := "postgres://jarvis:kept@127.0.0.1:25432/jarvis?sslmode=disable"
	if err != nil || created || got != want || f.count("docker run") != 0 {
		t.Fatalf("existing: %q %v %v", got, created, err)
	}
	if f.count("docker exec persona-mgr-m-0a1b2c-pg pg_isready -h 127.0.0.1 -p 5432 -U jarvis -d jarvis") == 0 {
		t.Fatalf("asked as another role:\n%s", strings.Join(f.calls, "\n"))
	}
	// its port taken meanwhile: made again over the same volume with its own role and database
	f.taken = 25432
	o.Password = "kept"
	moved, _, err := ProvisionManagerDB(ctx, "m-0a1b2c", o)
	if err != nil || moved != "postgres://jarvis:kept@127.0.0.1:25433/jarvis?sslmode=disable" || f.user != "jarvis" ||
		f.count("docker run -d --name persona-mgr-m-0a1b2c-pg --restart unless-stopped -p 127.0.0.1:25433:5432 -v persona-mgr-m-0a1b2c-data:/var/lib/postgresql -e POSTGRES_USER=jarvis -e POSTGRES_PASSWORD -e POSTGRES_DB=jarvis") != 1 {
		t.Fatalf("moved: %q %v\n%s", moved, err, strings.Join(f.calls, "\n"))
	}
	// a new one has today's names
	if c := (&Container{}); c.URL("pw", 1) != URL("pw", 1) || !strings.Contains(URL("pw", 1), "//"+User+":pw@") {
		t.Fatalf("new: %q", URL("pw", 1))
	}
}
