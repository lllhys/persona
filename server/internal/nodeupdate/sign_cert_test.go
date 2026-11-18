package nodeupdate

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/nodesign"
)

// PERSONA_TEST_CODESIGN_IDENTITY names a code signing certificate in this Mac's login keychain (run in the login
// session, as the resident node runs): a build another path put in place and an update both end up signed with it
// under the fixed identifier, with one designated requirement, and the update's transaction confirms the signed file.
func TestRealCertificateSigning(t *testing.T) {
	identity := os.Getenv("PERSONA_TEST_CODESIGN_IDENTITY")
	if runtime.GOOS != "darwin" || identity == "" {
		t.Skip("PERSONA_TEST_CODESIGN_IDENTITY names no certificate (a Mac's login session)")
	}
	// codesign finds the login keychain through HOME, which TestMain points at the fixtures.
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", u.HomeDir)
	ctx := context.Background()
	signer := nodesign.New(nodesign.Config{Identity: identity}, "personad")
	m, data, _ := testManager(t)
	logs := &signLog{}
	m.signer, m.logf = signer, logs.logf
	if m.identity, err = os.Stat(m.exe); err != nil {
		t.Fatal(err)
	}
	codesign := func(args ...string) string {
		t.Helper()
		b, err := exec.Command("/usr/bin/codesign", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("codesign %q: %v %s", args, err, b)
		}
		return string(b)
	}
	if signer.Meets(ctx, m.exe) {
		t.Fatal("the linker-signed fixture meets the requirement")
	}
	if !m.SignInstalled(ctx) {
		t.Fatalf("the installed build was not signed: %q", logs.lines)
	}
	installed := codesign("-d", "-r-", m.exe)
	t.Logf("installed build, signed at start: %s%s", codesign("-dv", m.exe), installed)
	id := receive(t, m, data, spec(data, "next"))
	if _, err := m.Commit(ctx, id, nil); err != nil {
		t.Fatal(err)
	}
	updated := codesign("-d", "-r-", m.exe)
	t.Logf("update, signed before the swap: %s%s", codesign("-dv", m.exe), updated)
	if !signer.Meets(ctx, m.exe) || !strings.Contains(updated, `designated => identifier "com.nwverse.personad" and `) {
		t.Fatalf("the update is not signed with the certificate: %q", logs.lines)
	}
	if updated != installed {
		t.Fatalf("the designated requirement changed with the update:\n%s\n%s", installed, updated)
	}
	if check, err := runCheck(ctx, m.exe); err != nil || check.Version != "next" {
		t.Fatalf("the signed update does not run: %+v %v", check, err)
	}
	running := &Manager{exe: m.exe, version: "next", startWatch: func(string, string, string) error { return nil }}
	if err := running.recover(); err != nil {
		t.Fatal(err)
	}
	if err := running.Confirm("next"); err != nil {
		t.Fatal(err)
	}
	t.Logf("log: %q", logs.lines)
}
