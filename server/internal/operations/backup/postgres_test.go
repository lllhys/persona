package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func environmentMap(t *testing.T, environment []string) map[string]string {
	t.Helper()
	values := map[string]string{}
	for _, variable := range environment {
		name, value, ok := strings.Cut(variable, "=")
		if !ok || values[name] != "" {
			fatalf(t, "malformed or duplicate variable %q in %s", variable, shownEnvironment(environment))
		}
		values[name] = value
	}
	return values
}

// isolateLibpqEnvironment keeps the developer's own PG* variables and passfile
// out of pgx's defaults.
func isolateLibpqEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"PGHOST", "PGPORT", "PGUSER", "PGDATABASE", "PGPASSWORD", "PGSSLMODE", "PGSERVICE", "PGAPPNAME"} {
		t.Setenv(name, "")
	}
	t.Setenv("PGPASSFILE", filepath.Join(t.TempDir(), "none"))
}

// libpq expands a URI only as an explicit dbname argument, never from
// PGDATABASE: every connection part must be its own variable.
func TestClientEnvironmentUsesPlainLibpqVariables(t *testing.T) {
	isolateLibpqEnvironment(t)
	target := Target{URL: "postgres://persona:process-secret@db.example:6543/persona?sslmode=verify-full&sslrootcert=/etc/persona/root.crt&password=query-secret&connect_timeout=7"}
	values := environmentMap(t, clientEnvironment(target, "another database", false))
	want := map[string]string{
		"PGHOST": "db.example", "PGPORT": "6543", "PGUSER": "persona", "PGDATABASE": "another database",
		"PGSSLMODE": "verify-full", "PGSSLROOTCERT": "/etc/persona/root.crt", "PGCONNECT_TIMEOUT": "7",
	}
	for name, value := range want {
		if values[name] != value {
			errorf(t, "%s = %q, want %q", name, values[name], value)
		}
	}
	if values["PGPASSWORD"] != "query-secret" && values["PGPASSWORD"] != "process-secret" {
		errorf(t, "PGPASSWORD = %q", values["PGPASSWORD"])
	}
	for name, value := range values {
		if name == "PGPASSWORD" {
			continue
		}
		if strings.Contains(value, "secret") || strings.Contains(value, "://") {
			errorf(t, "%s carries a password or URL: %q", name, value)
		}
	}
}

func TestClientEnvironmentKeepsPasswordOutOfDatabaseArgument(t *testing.T) {
	isolateLibpqEnvironment(t)
	target := Target{URL: "postgres://persona:process-secret@db.example:5432/persona?sslmode=require&sslrootcert=/host/only.crt"}
	for _, docker := range []bool{false, true} {
		values := environmentMap(t, clientEnvironment(target, "another database", docker))
		if values["PGPASSWORD"] != "process-secret" || values["PGDATABASE"] != "another database" || values["PGUSER"] != "persona" {
			fatalf(t, "docker=%v client environment = %s", docker, shownEnvironment(values))
		}
		if docker {
			if values["PGHOST"] != "127.0.0.1" || values["PGPORT"] != "5432" || values["PGSSLMODE"] != "disable" || values["PGSSLROOTCERT"] != "" {
				fatalf(t, "Docker client does not use the container's loopback: %s", shownEnvironment(values))
			}
		}
	}
}

func TestClientEnvironmentUnixSocketAndDefaultHost(t *testing.T) {
	isolateLibpqEnvironment(t)
	values := environmentMap(t, clientEnvironment(Target{URL: "postgres://persona@/persona?host=/run/persona-pg"}, "persona", false))
	if values["PGHOST"] != "/run/persona-pg" || values["PGPORT"] != "5432" || values["PGUSER"] != "persona" || values["PGPASSWORD"] != "" {
		fatalf(t, "unix-socket environment = %s", shownEnvironment(values))
	}
	values = environmentMap(t, clientEnvironment(Target{URL: "postgres:///persona"}, "persona", false))
	if values["PGHOST"] == "" || values["PGDATABASE"] != "persona" {
		fatalf(t, "missing host should fall back to pgx's default host: %s", shownEnvironment(values))
	}
}

func TestRunClientRedactsRealProcessErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		skip(t, "POSIX test executable")
	}
	directory := t.TempDir()
	secret := "process-secret"
	rawURL := "postgres://persona:" + secret + "@db.example:5432/persona?sslmode=disable"
	script := "#!/bin/sh\nprintf '%s %s\\n' '" + rawURL + "' '" + secret + "' >&2\nexit 1\n"
	name := filepath.Join(directory, "pg_dump")
	if err := os.WriteFile(name, []byte(script), 0o700); err != nil {
		fatal(t, err)
	}
	t.Setenv("PATH", directory)
	backend := newPostgresBackend()
	err := backend.runClient(context.Background(), Target{URL: rawURL}, "persona", nil, io.Discard, "pg_dump")
	if err == nil {
		fatal(t, "expected the fake client failure")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), rawURL) {
		fatalf(t, "process error leaked credentials: %q", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		fatalf(t, "process error was not redacted: %q", err)
	}
}

func TestRunClientDockerArgumentsContainNoPassword(t *testing.T) {
	if runtime.GOOS == "windows" {
		skip(t, "POSIX test executable")
	}
	directory := t.TempDir()
	argsFile := filepath.Join(directory, "args")
	databaseFile := filepath.Join(directory, "database")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >\"$CAPTURE_ARGS\"\nprintf '%s' \"$PGDATABASE\" >\"$CAPTURE_DATABASE\"\nprintf 'dump'\n"
	if err := os.WriteFile(filepath.Join(directory, "docker"), []byte(script), 0o700); err != nil {
		fatal(t, err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("CAPTURE_ARGS", argsFile)
	t.Setenv("CAPTURE_DATABASE", databaseFile)
	backend := newPostgresBackend()
	target := Target{URL: "postgres://persona:process-secret@db.example:5432/persona?sslmode=disable", Container: "disposable-postgres"}
	var stdout bytes.Buffer
	if err := backend.runClient(context.Background(), target, "persona", nil, &stdout, "pg_dump", "--format=custom"); err != nil {
		fatal(t, err)
	}
	args, _ := os.ReadFile(argsFile)
	database, _ := os.ReadFile(databaseFile)
	if strings.Contains(string(args), "process-secret") || strings.Contains(string(database), "process-secret") {
		fatalf(t, "Docker arguments leak a password: args %q database %q", args, database)
	}
	for _, wanted := range []string{"exec", "--interactive", "disposable-postgres", "pg_dump", "--format=custom"} {
		if !strings.Contains(string(args), wanted) {
			errorf(t, "Docker arguments lack %q: %q", wanted, args)
		}
	}
	if string(database) != "persona" {
		errorf(t, "PGDATABASE = %q, want the plain database name", database)
	}
	// Every variable set for the client must be forwarded into the container.
	for _, variable := range clientEnvironment(target, "persona", true) {
		name, _, _ := strings.Cut(variable, "=")
		if !strings.Contains(string(args), "--env\n"+name+"\n") {
			errorf(t, "Docker arguments do not forward %s: %q", name, args)
		}
	}
	if stdout.String() != "dump" {
		fatalf(t, "dump stdout = %q", stdout.String())
	}
}

func TestExternalDatabaseDoesNotGuessLocalDockerContainer(t *testing.T) {
	target, err := makeTarget("hub", "postgres://persona:secret@db.example:5432/persona", "", "persona-postgres")
	if err != nil {
		fatal(t, err)
	}
	if target.DefaultContainer != "" {
		fatalf(t, "external URL guessed container %q", target.DefaultContainer)
	}
	local, err := makeTarget("hub", "postgres://persona:secret@127.0.0.1:5432/persona", "", "persona-postgres")
	if err != nil {
		fatal(t, err)
	}
	if local.DefaultContainer != "persona-postgres" {
		fatalf(t, "native Docker default was not retained: %q", local.DefaultContainer)
	}
}

// pg_restore without -d prints a script instead of connecting (PostgreSQL 18
// refuses outright), so restore names the plain database explicitly.
func TestRestorePassesPlainDatabaseName(t *testing.T) {
	if runtime.GOOS == "windows" {
		skip(t, "POSIX test executable")
	}
	directory := t.TempDir()
	argsFile := filepath.Join(directory, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >\"$CAPTURE_ARGS\"\n"
	if err := os.WriteFile(filepath.Join(directory, "pg_restore"), []byte(script), 0o700); err != nil {
		fatal(t, err)
	}
	dump := filepath.Join(directory, "dump")
	if err := os.WriteFile(dump, []byte("dump"), 0o600); err != nil {
		fatal(t, err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("CAPTURE_ARGS", argsFile)
	backend := newPostgresBackend()
	target := Target{URL: "postgres://persona:process-secret@db.example:5432/persona?sslmode=disable"}
	if err := backend.Restore(context.Background(), target, "persona_verify_m_0123", dump); err != nil {
		fatal(t, err)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "--dbname=persona_verify_m_0123\n") || strings.Contains(string(args), "secret") {
		fatalf(t, "pg_restore arguments = %q", args)
	}
	for _, unsafe := range []string{"", "host=elsewhere dbname=x", "postgres://elsewhere/x"} {
		if err := backend.Restore(context.Background(), target, unsafe, dump); err == nil {
			errorf(t, "restore accepted database name %q", unsafe)
		}
	}
}

// The decision to fall back to the local container must use the host the
// connection really uses (pgx's), not the URL authority: here the authority
// says localhost but the query moves the connection to another server.
func TestContainerFallbackFollowsTheConnectionHost(t *testing.T) {
	isolateLibpqEnvironment(t)
	for raw, want := range map[string]string{
		"postgres://u@localhost/db?host=remote.example":           "",
		"postgres://u@127.0.0.1/db?host=remote.example":           "",
		"postgres://u@localhost/db?host=127.0.0.1,remote.example": "",
		"postgres://u@/db?host=remote.example":                    "",
		"postgres://u@remote.example/db?host=127.0.0.1":           "persona-postgres",
		"postgres://u@127.0.0.1/db":                               "persona-postgres",
		"postgres://u@localhost:5433/db":                          "persona-postgres",
	} {
		target, err := makeTarget("hub", raw, "", "persona-postgres")
		if err != nil {
			fatalf(t, "makeTarget(%q): %v", raw, err)
		}
		if target.DefaultContainer != want {
			errorf(t, "makeTarget(%q).DefaultContainer = %q, want %q", raw, target.DefaultContainer, want)
		}
	}
}

// pgx reads sslrootcert/sslcert while parsing; a file that is not readable
// where personad runs must not turn into "must name a database".
func TestMakeTargetIgnoresUnreadableTLSFiles(t *testing.T) {
	isolateLibpqEnvironment(t)
	raw := "postgres://u@127.0.0.1/db?sslmode=verify-full&sslrootcert=/nonexistent/ca.pem&sslcert=/nonexistent/c.pem&sslkey=/nonexistent/k.pem"
	target, err := makeTarget("hub", raw, "", "persona-postgres")
	if err != nil {
		fatalf(t, "makeTarget: %v", err)
	}
	if target.Database != "db" || target.DefaultContainer != "persona-postgres" {
		fatalf(t, "target = %+v", target)
	}
	if _, err := makeTarget("hub", "postgres://u@127.0.0.1/?sslrootcert=/nonexistent/ca.pem", "", ""); err == nil || !strings.Contains(err.Error(), "must name a database") {
		fatalf(t, "a URL without a database = %v", err)
	}
}

// A non-loopback host (the shipped Compose form: host "postgres") with no
// pg_dump/pg_restore here and no usable container fails before anything is
// done, with a message that says so.
func TestClientPreflightFailsEarlyWithoutUsableClients(t *testing.T) {
	isolateLibpqEnvironment(t)
	missing := func(string) (string, error) { return "", os.ErrNotExist }
	present := func(name string) (string, error) { return "/usr/bin/" + name, nil }
	compose, err := makeTarget("hub", "postgres://persona@postgres:5432/persona", "", "persona-postgres")
	if err != nil {
		fatal(t, err)
	}
	backend := &postgresBackend{lookPath: missing}
	err = backend.CheckClients(compose, "pg_dump", "pg_restore")
	if err == nil {
		fatal(t, "a non-loopback host without clients was accepted")
	}
	for _, wanted := range []string{"pg_dump", "postgres", "Compose", "not supported"} {
		if !strings.Contains(err.Error(), wanted) {
			errorf(t, "error lacks %q: %v", wanted, err)
		}
	}
	if strings.Contains(err.Error(), "persona@") {
		errorf(t, "error leaks the URL: %v", err)
	}
	only := func(name string) (string, error) {
		if name == "pg_dump" {
			return "/usr/bin/pg_dump", nil
		}
		return "", os.ErrNotExist
	}
	if err := (&postgresBackend{lookPath: only}).CheckClients(compose, "pg_dump", "pg_restore"); err == nil || !strings.Contains(err.Error(), "pg_restore") {
		errorf(t, "pg_restore missing = %v", err)
	}
	if err := (&postgresBackend{lookPath: only}).CheckClients(compose, "pg_dump"); err != nil {
		errorf(t, "only pg_dump needed and present: %v", err)
	}
	if err := (&postgresBackend{lookPath: present}).CheckClients(compose, "pg_dump", "pg_restore"); err != nil {
		errorf(t, "clients present: %v", err)
	}
	// The loopback default container needs docker itself.
	local, err := makeTarget("hub", "postgres://persona@127.0.0.1:5432/persona", "", "persona-postgres")
	if err != nil {
		fatal(t, err)
	}
	if err := (&postgresBackend{lookPath: missing}).CheckClients(local, "pg_dump"); err == nil || !strings.Contains(err.Error(), "docker") {
		errorf(t, "no clients and no docker = %v", err)
	}
	dockerOnly := func(name string) (string, error) {
		if name == "docker" {
			return "/usr/bin/docker", nil
		}
		return "", os.ErrNotExist
	}
	if err := (&postgresBackend{lookPath: dockerOnly}).CheckClients(local, "pg_dump", "pg_restore"); err != nil {
		errorf(t, "container fallback available: %v", err)
	}
}

// The redaction list must hold the query-string password even when TLS files
// named in the URL cannot be read here.
func TestTargetSecretsIncludeQueryPasswordWithUnreadableTLSFiles(t *testing.T) {
	isolateLibpqEnvironment(t)
	target := Target{URL: "postgres://u@db.example/db?sslmode=verify-full&sslrootcert=/nonexistent/ca.pem&password=query-secret"}
	found := false
	for _, secret := range targetSecrets(target) {
		found = found || secret == "query-secret"
	}
	if !found {
		fatalf(t, "targetSecrets = %q", targetSecrets(target))
	}
}

// With an explicit container the programs are not looked up here, so a missing
// docker command must not be reported as missing client tools.
func TestPreflightDockerMessageIsAccurate(t *testing.T) {
	isolateLibpqEnvironment(t)
	target, err := makeTarget("hub", "postgres://u@db.example/db", "named-container", "")
	if err != nil {
		fatal(t, err)
	}
	err = (&postgresBackend{lookPath: func(string) (string, error) { return "", os.ErrNotExist }}).CheckClients(target, "pg_dump")
	if err == nil || !strings.Contains(err.Error(), "docker") || !strings.Contains(err.Error(), "named-container") {
		fatalf(t, "error = %v", err)
	}
	if strings.Contains(err.Error(), "not installed here") {
		errorf(t, "the message claims the clients are missing without checking: %v", err)
	}
	// Clients on PATH while docker is missing: the explicit container still needs docker.
	only := func(name string) (string, error) {
		if name == "pg_dump" {
			return "/bin/pg_dump", nil
		}
		return "", os.ErrNotExist
	}
	err = (&postgresBackend{lookPath: only}).CheckClients(target, "pg_dump")
	if err == nil || strings.Contains(err.Error(), "not installed here") {
		fatalf(t, "clients present, docker missing = %v", err)
	}
}

// The default container needs docker too; without it the message names what is
// missing and the container.
func TestPreflightMissingDockerForTheDefaultContainerNamesBoth(t *testing.T) {
	isolateLibpqEnvironment(t)
	local, err := makeTarget("hub", "postgres://persona@127.0.0.1:5432/persona", "", "persona-postgres")
	if err != nil {
		fatal(t, err)
	}
	only := func(name string) (string, error) {
		if name == "pg_dump" {
			return "/bin/pg_dump", nil
		}
		return "", os.ErrNotExist
	}
	err = (&postgresBackend{lookPath: only}).CheckClients(local, "pg_dump", "pg_restore")
	if err == nil {
		fatal(t, "missing pg_restore and docker was accepted")
	}
	for _, wanted := range []string{"pg_restore", "docker", "persona-postgres"} {
		if !strings.Contains(err.Error(), wanted) {
			errorf(t, "error lacks %q: %v", wanted, err)
		}
	}
	if strings.Contains(err.Error(), "pg_dump and") || strings.Contains(err.Error(), "and pg_restore") {
		errorf(t, "error names a program that is present: %v", err)
	}
}

// pgx tries every host of a multi-host URL in order (cfg.Fallbacks); the
// command-line clients must be given the same list, each host with its own
// port, or a backup fails whenever the first host is down or is not the one
// target_session_attrs wants.
func TestClientEnvironmentKeepsEveryHostOfAMultiHostURL(t *testing.T) {
	isolateLibpqEnvironment(t)
	target := Target{URL: "postgres://persona:pw@db-a.example:5433,db-b.example:6544,db-c.example:7655/persona?target_session_attrs=read-write&sslmode=disable"}
	values := environmentMap(t, clientEnvironment(target, "persona", false))
	if values["PGHOST"] != "db-a.example,db-b.example,db-c.example" || values["PGPORT"] != "5433,6544,7655" || values["PGTARGETSESSIONATTRS"] != "read-write" {
		fatalf(t, "multi-host environment = %s", shownEnvironment(values))
	}
	values = environmentMap(t, clientEnvironment(Target{URL: "postgres://persona@/persona?host=/run/pg-a,/run/pg-b&port=5433,5434"}, "persona", false))
	if values["PGHOST"] != "/run/pg-a,/run/pg-b" || values["PGPORT"] != "5433,5434" {
		fatalf(t, "multi-socket environment = %s", shownEnvironment(values))
	}
	// Inside the container the server is the container's own loopback.
	values = environmentMap(t, clientEnvironment(target, "persona", true))
	if values["PGHOST"] != "127.0.0.1" || values["PGPORT"] != "5432" {
		fatalf(t, "docker environment = %s", shownEnvironment(values))
	}
}

// pgx decrypts an encrypted TLS client key with sslpassword. libpq has no
// environment variable for it (its PQconninfoOptions entry names none), so the
// clients get it in a private service file: never in their arguments or
// environment, the file gone once they exit, and the value kept out of every
// error.
func TestRunClientPassesTheTLSKeyPasswordPrivately(t *testing.T) {
	if runtime.GOOS == "windows" {
		skip(t, "POSIX test executable")
	}
	isolateLibpqEnvironment(t)
	directory := t.TempDir()
	report := filepath.Join(directory, "report")
	secret := "key-passphrase-secret"
	rawURL := "postgres://persona@db.example:5432/persona?sslmode=verify-full&sslkey=/etc/persona/client.key&sslcert=/etc/persona/client.crt&sslpassword=" + secret
	// The script reports whether the passphrase is anywhere in its environment
	// and PGSERVICE, never the environment itself.
	script := "#!/bin/sh\n{ echo \"ARGS $*\"; " +
		"if awk 'BEGIN { for (name in ENVIRON) if (index(ENVIRON[name], \"" + secret + "\")) found = 1; exit !found }'; then echo 'ENVIRONMENT HAS THE KEY PASSWORD " + secret + "'; fi; " +
		"echo \"PGSERVICE=$PGSERVICE\"; echo SERVICE; cat \"$PGSERVICEFILE\"; echo \"FILE $PGSERVICEFILE\"; } > '" + report + "'\n" +
		"echo \"could not decrypt with sslpassword=" + secret + " (" + secret + ")\" >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(directory, "pg_dump"), []byte(script), 0o700); err != nil {
		fatal(t, err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+"/usr/bin:/bin")
	err := newPostgresBackend().runClient(context.Background(), Target{URL: rawURL}, "persona", nil, io.Discard, "pg_dump", "--format=custom")
	if err == nil {
		fatal(t, "expected the fake client failure")
	}
	if strings.Contains(err.Error(), secret) {
		errorf(t, "the error leaks the key password: %q", err)
	}
	raw, readErr := os.ReadFile(report)
	if readErr != nil {
		fatal(t, readErr)
	}
	before, service, _ := strings.Cut(string(raw), "\nSERVICE\n")
	if strings.Contains(before, secret) {
		errorf(t, "the key password is in the client's arguments or environment:\n%s", before)
	}
	if !strings.Contains(before, "PGSERVICE=persona-backup") || !strings.Contains(service, "[persona-backup]\nsslpassword="+secret+"\n") {
		fatalf(t, "the client was not given the key password through its service file:\n%s", raw)
	}
	_, file, _ := strings.Cut(service, "FILE ")
	if _, statErr := os.Stat(strings.TrimSpace(file)); !os.IsNotExist(statErr) {
		errorf(t, "the service file %s remains: %v", strings.TrimSpace(file), statErr)
	}
	if got := targetError(Target{URL: rawURL}, "dump", errors.New("connect with "+secret+" failed")).Error(); strings.Contains(got, secret) {
		errorf(t, "a target error leaks the key password: %q", got)
	}
	// Without sslpassword no service file is made.
	if environment, cleanup, err := clientServiceFile(Target{URL: "postgres://persona@db.example/persona"}); err != nil || len(environment) != 0 {
		errorf(t, "service file for a URL without sslpassword: %v, %v", environment, err)
	} else {
		cleanup()
	}
	// A value a service file cannot hold is refused, without echoing it.
	if _, _, err := clientServiceFile(Target{URL: "postgres://persona@db.example/persona?sslpassword=abc%0Axyz"}); err == nil || strings.Contains(err.Error(), "xyz") {
		errorf(t, "a line break in sslpassword: %v", err)
	}
}

// pgx reads the first value of a repeated URL query parameter; the clients
// must be given the same one, sslpassword included.
func TestClientsTakeTheFirstValueOfARepeatedParameterAsPgxDoes(t *testing.T) {
	isolateLibpqEnvironment(t)
	rawURL := "postgres://persona@db.example/persona?sslmode=require&sslmode=disable&sslrootcert=/first.crt&sslrootcert=/second.crt&sslpassword=first-pass&sslpassword=second-pass"
	cfg, err := pgx.ParseConfig("postgres://persona@db.example/persona?sslmode=require&sslmode=disable&sslpassword=first-pass&sslpassword=second-pass")
	if err != nil {
		fatal(t, err)
	}
	if cfg.TLSConfig == nil {
		fatal(t, "pgx no longer takes the first sslmode (require); revisit which value the clients get")
	}
	values := environmentMap(t, clientEnvironment(Target{URL: rawURL}, "persona", false))
	if values["PGSSLMODE"] != "require" || values["PGSSLROOTCERT"] != "/first.crt" {
		errorf(t, "the clients get PGSSLMODE=%s PGSSLROOTCERT=%s, want the first values (require, /first.crt)", values["PGSSLMODE"], values["PGSSLROOTCERT"])
	}
	environment, cleanup, err := clientServiceFile(Target{URL: rawURL})
	if err != nil {
		fatal(t, err)
	}
	defer cleanup()
	service := environmentMap(t, environment)["PGSERVICEFILE"]
	raw, err := os.ReadFile(service)
	if err != nil {
		fatal(t, err)
	}
	if !strings.Contains(string(raw), "sslpassword=first-pass\n") || strings.Contains(string(raw), "second-pass") {
		errorf(t, "the service file does not hold the first sslpassword only (%d bytes)", len(raw))
	}
	// An empty first value is no password, as for pgx.
	if environment, cleanup, err := clientServiceFile(Target{URL: "postgres://persona@db.example/persona?sslpassword=&sslpassword=second-pass"}); err != nil || len(environment) != 0 {
		errorf(t, "an empty first sslpassword gave a service file: %v", err)
	} else {
		cleanup()
	}
}

// The service file is mode 0600 in a mode-0700 directory of its own, and a
// value it cannot hold (trailing white space, which libpq drops, or a line
// break) is refused without being echoed.
func TestClientServiceFileModesAndRefusals(t *testing.T) {
	if runtime.GOOS == "windows" {
		skip(t, "POSIX file modes")
	}
	environment, cleanup, err := clientServiceFile(Target{URL: "postgres://persona@db.example/persona?sslpassword=pass-value"})
	if err != nil {
		fatal(t, err)
	}
	service := environmentMap(t, environment)["PGSERVICEFILE"]
	file, fileErr := os.Stat(service)
	directory, dirErr := os.Stat(filepath.Dir(service))
	if fileErr != nil || dirErr != nil || file.Mode().Perm() != 0o600 || directory.Mode().Perm() != 0o700 {
		errorf(t, "service file mode %v (%v) in directory mode %v (%v), want 0600 in 0700", file.Mode().Perm(), fileErr, directory.Mode().Perm(), dirErr)
	}
	if environmentMap(t, environment)["PGSERVICE"] != "persona-backup" {
		errorf(t, "PGSERVICE is not set to the file's service")
	}
	cleanup()
	if _, err := os.Stat(filepath.Dir(service)); !errors.Is(err, os.ErrNotExist) {
		errorf(t, "the service directory remains after cleanup: %v", err)
	}
	for _, value := range []string{"qx7space%20", "qx7tab%09", "qx7line%0Abreak", "qx7carriage%0Dreturn", "qx7nul%00byte"} {
		_, cleanup, err := clientServiceFile(Target{URL: "postgres://persona@db.example/persona?sslpassword=" + value})
		cleanup()
		if err == nil || !strings.Contains(err.Error(), "sslpassword") || strings.Contains(err.Error(), strings.SplitN(value, "%", 2)[0]) {
			errorf(t, "sslpassword %d bytes long: refusal %v", len(value), err)
		}
	}
	// Leading white space is kept by libpq, so it is accepted.
	if _, cleanup, err := clientServiceFile(Target{URL: "postgres://persona@db.example/persona?sslpassword=%20leading"}); err != nil {
		errorf(t, "a leading space was refused: %v", err)
	} else {
		cleanup()
	}
}
