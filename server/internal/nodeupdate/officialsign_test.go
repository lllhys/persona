package nodeupdate

import (
	"context"
	"errors"
	"runtime"
	"testing"
)

func TestOfficialSignaturePreservedWithoutResigning(t *testing.T) {
	oldReq, oldVerify := officialRequirement, officialVerify
	t.Cleanup(func() { officialRequirement, officialVerify = oldReq, oldVerify })
	officialRequirement = func(context.Context, string) ([]byte, error) {
		return []byte(`designated => identifier "com.nwverse.personad" and anchor apple generic and certificate leaf[subject.OU] = "TEAM"`), nil
	}
	officialVerify = func(context.Context, string) error { return nil }
	m, data, logs := signingManager(t, &fakeSigner{})
	id := receive(t, m, data, spec(data, "next"))
	if _, err := m.Commit(context.Background(), id, nil); err != nil {
		t.Fatal(err)
	}
	// Only macOS has codesign: elsewhere OfficialSigned is false, so the update is signed as any other (#9796).
	if runtime.GOOS != "darwin" {
		if logs.has("keeps its official release signature") || !logs.has("installs signed with the certificate") {
			t.Fatalf("off macOS the update must be signed as usual: %q", logs.lines)
		}
		return
	}
	if !logs.has("keeps its official release signature") {
		t.Fatalf("preservation not logged: %q", logs.lines)
	}
}

func TestOfficialSignatureMustVerifyBeforePreservation(t *testing.T) {
	oldReq, oldVerify := officialRequirement, officialVerify
	t.Cleanup(func() { officialRequirement, officialVerify = oldReq, oldVerify })
	officialRequirement = func(context.Context, string) ([]byte, error) {
		return []byte(`designated => identifier "com.nwverse.personad" and anchor apple generic and certificate leaf[subject.OU] = "TEAM"`), nil
	}
	officialVerify = func(context.Context, string) error { return errors.New("broken signature") }
	if OfficialSigned(context.Background(), "/tmp/example") || preserveOfficial(context.Background(), "/tmp/example") {
		t.Fatal("accepted an invalid official signature")
	}
}

func TestAdHocIsNotAnOfficialSignature(t *testing.T) {
	oldReq := officialRequirement
	t.Cleanup(func() { officialRequirement = oldReq })
	officialRequirement = func(context.Context, string) ([]byte, error) {
		return []byte(`designated => cdhash H"1234"`), nil
	}
	if OfficialSigned(context.Background(), "/tmp/example") {
		t.Fatal("accepted ad-hoc cdhash as an official signature")
	}
}
