package nodesign

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if c, err := Load(filepath.Join(dir, "missing.json")); err != nil || c != (Config{}) {
		t.Fatalf("no file: %+v %v", c, err)
	}
	cases := []struct {
		body string
		want Config
		bad  string
	}{
		{body: `{"identity": " Apple Development: Someone (TEAMID1234) "}`, want: Config{Identity: "Apple Development: Someone (TEAMID1234)"}},
		{body: `{"identity": "0123456789ABCDEF0123456789abcdef01234567", "identifier": "org.example.node"}`, want: Config{Identity: "0123456789ABCDEF0123456789abcdef01234567", Identifier: "org.example.node"}},
		{body: `{"identity": ""}`, bad: "empty"},
		{body: `{}`, bad: "empty"},
		{body: `{"identity": "-"}`, bad: "ad hoc"},
		{body: `{"identity": "Name \"quoted\""}`, bad: "not a certificate name"},
		{body: `{"identity": "line\nbreak"}`, bad: "not a certificate name"},
		{body: `{"identity": "Apple Development: Someone", "identifier": "com.example/node"}`, bad: "letters, digits"},
		{body: `{"identity": `, bad: "unexpected end"},
	}
	for i, tc := range cases {
		path := filepath.Join(dir, fmt.Sprintf("%d.json", i))
		if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if tc.bad != "" {
			if err == nil || !strings.Contains(err.Error(), tc.bad) {
				t.Errorf("%s: want an error about %q, got %+v %v", tc.body, tc.bad, c, err)
			}
			continue
		}
		if err != nil || c != tc.want {
			t.Errorf("%s: got %+v %v", tc.body, c, err)
		}
	}
}

func TestNoSettingNoSigner(t *testing.T) {
	if s := New(Config{}, "personad"); s != nil {
		t.Fatal("a signer without a certificate")
	}
	if runtime.GOOS != "darwin" {
		if s := New(Config{Identity: "Apple Development: Someone"}, "personad"); s != nil {
			t.Fatal("a signer off a Mac")
		}
		if s, err := FromFile("personad"); s != nil || err != nil {
			t.Fatalf("FromFile off a Mac: %v %v", s, err)
		}
	}
}

func TestIdentifierAndRequirement(t *testing.T) {
	byName := newSigner(Config{Identity: "Apple Development: Someone (TEAMID1234)"}, "personad", nil)
	if byName.Identifier() != "com.nwverse.personad" {
		t.Fatal(byName.Identifier())
	}
	if got, want := byName.Requirement(), `identifier "com.nwverse.personad" and certificate leaf[subject.CN] = "Apple Development: Someone (TEAMID1234)"`; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	byHash := newSigner(Config{Identity: "0123456789ABCDEF0123456789ABCDEF01234567"}, "persona-node", nil)
	if got, want := byHash.Requirement(), `identifier "com.nwverse.persona-node" and certificate leaf = H"0123456789abcdef0123456789abcdef01234567"`; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	own := newSigner(Config{Identity: "x", Identifier: "org.example.node"}, "personad", nil)
	if own.Identifier() != "org.example.node" || DefaultIdentifier("") != "com.nwverse.personad" {
		t.Fatal(own.Identifier(), DefaultIdentifier(""))
	}
}

// fakeCodesign records codesign's arguments and answers with what fail says for the step whose first argument it is.
type fakeCodesign struct {
	calls [][]string
	fail  map[string]struct {
		out string
		err error
	}
}

func (f *fakeCodesign) run(_ context.Context, _ time.Duration, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	key := args[0]
	if key == "--force" {
		key = args[2] // the identity: "-" for ad hoc
	}
	if r, ok := f.fail[key]; ok {
		return []byte(r.out), r.err
	}
	return nil, nil
}

func TestSignRunsCodesign(t *testing.T) {
	f := &fakeCodesign{}
	s := newSigner(Config{Identity: "Apple Development: Someone (TEAMID1234)"}, "personad", f.run)
	if err := s.Sign(context.Background(), "/tmp/stage"); err != nil {
		t.Fatal(err)
	}
	if err := s.AdHoc(context.Background(), "/tmp/stage"); err != nil {
		t.Fatal(err)
	}
	if !s.Meets(context.Background(), "/tmp/stage") {
		t.Fatal("does not meet")
	}
	want := [][]string{
		{"--force", "--sign", "Apple Development: Someone (TEAMID1234)", "--identifier", "com.nwverse.personad", "--timestamp=none", "/tmp/stage"},
		{"--verify", "--strict", "-R", "=" + s.Requirement(), "/tmp/stage"},
		{"--force", "--sign", "-", "--identifier", "com.nwverse.personad", "/tmp/stage"},
		{"--verify", "--strict", "-R", `=identifier "com.nwverse.personad"`, "/tmp/stage"},
		{"--verify", "--strict", "-R", "=" + s.Requirement(), "/tmp/stage"},
	}
	if fmt.Sprint(f.calls) != fmt.Sprint(want) {
		t.Fatalf("codesign ran\n%q\nwant\n%q", f.calls, want)
	}
}

func TestSignFailuresSayWhatToDo(t *testing.T) {
	type result = struct {
		out string
		err error
	}
	exit := errors.New("exit status 1")
	cases := []struct {
		name string
		fail map[string]result
		want string
	}{
		{"keychain closed", map[string]result{"Apple Development: Someone": {"/tmp/stage: errSecInternalComponent", exit}}, "login keychain is not open"},
		{"no certificate", map[string]result{"Apple Development: Someone": {"Apple Development: Someone: no identity found", exit}}, "security find-identity"},
		{"dialog", map[string]result{"Apple Development: Someone": {"", fmt.Errorf("did not finish in 15s: %w", context.DeadlineExceeded)}}, "Always Allow"},
		{"partial name", map[string]result{"--verify": {"test-requirement: code failed to satisfy specified code requirement(s)", exit}}, "whole name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeCodesign{fail: tc.fail}
			err := newSigner(Config{Identity: "Apple Development: Someone"}, "personad", f.run).Sign(context.Background(), "/tmp/stage")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in %v", tc.want, err)
			}
		})
	}
}

// codesignTool skips off a Mac.
func codesignTool(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("codesign is macOS's")
	}
	if _, err := os.Stat("/usr/bin/codesign"); err != nil {
		t.Skip("no /usr/bin/codesign")
	}
}

// build compiles a small program into dir; tag makes each build different code (another cdhash).
func build(t *testing.T, dir, name, tag string) string {
	t.Helper()
	src := filepath.Join(dir, name+".go")
	if err := os.WriteFile(src, []byte("package main\n\nimport \"fmt\"\n\nvar tag = \"\"\n\nfunc main() { fmt.Println(\"build\", tag) }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", out, "-ldflags", "-X main.tag="+tag, src)
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v %s", err, b)
	}
	return out
}

// buildPersonad builds ./cmd/personad of this module at version into dir.
func buildPersonad(t *testing.T, dir, name, version string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", out, "-ldflags", "-X main.version="+version, "github.com/nwverse/persona/server/cmd/personad")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build personad: %v %s", err, b)
	}
	return out
}

func describe(t *testing.T, args ...string) string {
	t.Helper()
	b, err := exec.Command("/usr/bin/codesign", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("codesign %q: %v %s", args, err, b)
	}
	return string(b)
}

func field(text, key string) string {
	for _, line := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return v
		}
	}
	return ""
}

// designated is the designated requirement codesign -d -r- prints.
func designated(t *testing.T, path string) string {
	t.Helper()
	text := describe(t, "-d", "-r-", path)
	i := strings.Index(text, "designated => ")
	if i < 0 {
		t.Fatalf("no designated requirement in %q", text)
	}
	return strings.TrimSpace(text[i+len("designated => "):])
}

// What the linker signs and what AdHoc does keep TCC's grants keyed to the build: the designated requirement is the
// cdhash either way. Only the identifier changes.
func TestAdHocChangesOnlyTheName(t *testing.T) {
	codesignTool(t)
	dir := t.TempDir()
	path := build(t, dir, "personad", "1")
	if id := field(describe(t, "-dv", path), "Identifier"); id != "a.out" {
		t.Fatalf("the Go linker's identifier is %q now, not a.out: update the docs", id)
	}
	if dr := designated(t, path); !strings.HasPrefix(dr, "cdhash H\"") {
		t.Fatalf("linker-signed: %s", dr)
	}
	s := newSigner(Config{Identity: "unused"}, "personad", runCodesign)
	if err := s.AdHoc(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	info := describe(t, "-dv", path)
	if field(info, "Identifier") != "com.nwverse.personad" || field(info, "Signature") != "adhoc" {
		t.Fatalf("after AdHoc: %s", info)
	}
	if dr := designated(t, path); !strings.HasPrefix(dr, "cdhash H\"") {
		t.Fatalf("ad hoc under a fixed identifier: %s", dr)
	}
	if s.Meets(context.Background(), path) {
		t.Fatal("an ad-hoc signature meets the certificate's requirement")
	}
	if out, err := exec.Command(path).CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "build 1" {
		t.Fatalf("the re-signed build does not run: %v %s", err, out)
	}
}

// PERSONA_TEST_CODESIGN_IDENTITY names a code signing certificate in this Mac's login keychain (run in the login
// session): two different builds and a build signed twice all get the same designated requirement, made of the
// identifier and the certificate, while their cdhashes differ.
func TestCertificateKeepsTheRequirementAcrossBuilds(t *testing.T) {
	codesignTool(t)
	identity := os.Getenv("PERSONA_TEST_CODESIGN_IDENTITY")
	if identity == "" {
		t.Skip("PERSONA_TEST_CODESIGN_IDENTITY names no certificate")
	}
	s := New(Config{Identity: identity}, "personad")
	ctx := context.Background()
	dir := t.TempDir()
	first, second := build(t, dir, "first", "1"), build(t, dir, "second", "2")
	if os.Getenv("PERSONA_TEST_CODESIGN_PERSONAD") != "" {
		// personad itself, two versions of it (slower: a build of the whole program).
		first, second = buildPersonad(t, dir, "first", "v-sign-1"), buildPersonad(t, dir, "second", "v-sign-2")
	}
	if s.Meets(ctx, first) {
		t.Fatal("an unsigned build meets the requirement")
	}
	for _, path := range []string{first, second} {
		if err := s.Sign(ctx, path); err != nil {
			t.Fatal(err)
		}
	}
	again := filepath.Join(dir, "first-again")
	copyTo(t, first, again)
	if err := s.Sign(ctx, again); err != nil {
		t.Fatal(err)
	}
	var drs, cdhashes []string
	for _, path := range []string{first, second, again} {
		info := describe(t, "-dv", "--verbose=4", path)
		t.Logf("codesign -dv %s:\n%s", filepath.Base(path), info)
		if field(info, "Identifier") != "com.nwverse.personad" || field(info, "Signature") == "adhoc" ||
			field(info, "TeamIdentifier") == "" || field(info, "TeamIdentifier") == "not set" {
			t.Fatalf("%s is not signed with the certificate under the identifier", path)
		}
		dr := designated(t, path)
		t.Logf("codesign -d -r- %s: designated => %s", filepath.Base(path), dr)
		if !strings.HasPrefix(dr, `identifier "com.nwverse.personad" and `) || strings.Contains(dr, "cdhash") {
			t.Fatalf("designated requirement %s", dr)
		}
		if !s.Meets(ctx, path) {
			t.Fatalf("%s does not meet %s", path, s.Requirement())
		}
		drs, cdhashes = append(drs, dr), append(cdhashes, field(info, "CDHash"))
	}
	for _, path := range []string{first, second} {
		out, err := exec.Command(path, "version").CombinedOutput()
		if err != nil {
			t.Fatalf("the signed %s does not run: %v %s", path, err, out)
		}
		t.Logf("%s version: %s", filepath.Base(path), strings.TrimSpace(string(out)))
	}
	if drs[0] != drs[1] || drs[0] != drs[2] {
		t.Fatalf("the designated requirement changed: %q", drs)
	}
	if cdhashes[0] == cdhashes[1] || cdhashes[0] == "" {
		t.Fatalf("two builds, one cdhash: %q", cdhashes)
	}
}

func copyTo(t *testing.T, from, to string) {
	t.Helper()
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
