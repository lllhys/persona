package nodeupdate

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

var officialRequirement = func(ctx context.Context, path string) ([]byte, error) {
	return exec.CommandContext(ctx, "/usr/bin/codesign", "-d", "-r-", path).CombinedOutput()
}

var officialVerify = func(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--strict", path).Run()
}

// OfficialSigned says whether path already carries a certificate-backed
// designated requirement, not merely an ad-hoc cdhash. It deliberately does
// not select a particular team: that is the release publisher's trust policy,
// not proof by itself that this package is authentic.
func OfficialSigned(ctx context.Context, path string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	out, err := officialRequirement(ctx, path)
	if err != nil {
		return false
	}
	text := string(out)
	if !strings.Contains(text, "designated => identifier ") || !strings.Contains(text, "certificate") || strings.Contains(text, "cdhash") {
		return false
	}
	return officialVerify(ctx, path) == nil
}

// preserveOfficial keeps a certificate-backed signature already on a binary.
// A configured local certificate is not a license to replace another valid
// signature, and ad-hoc re-signing would break the release's permission grants.
func preserveOfficial(ctx context.Context, path string) bool {
	return OfficialSigned(ctx, path)
}

var errOfficialSignature = errors.New("the official signed binary did not verify")
