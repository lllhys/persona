package node

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestLauncherCheckModelOverrideReachesClaude(t *testing.T) {
	fakeClaudeHome(t, `model=""; while [ $# -gt 0 ]; do [ "$1" = --model ] && model=$2; shift; done
printf '{"type":"result","subtype":"success","is_error":false,"result":"ok","modelUsage":{"%s":{}}}\n' "$model"
`)
	home := os.Getenv("HOME")
	n := &Node{home: home, dir: filepath.Join(home, ".persona"), state: newState(), logger: log.New(io.Discard, "", 0)}
	res, err := nodeCall(t, n, "launchers.check", map[string]any{"name": "claude", "model": "opus"})
	if err != nil {
		t.Fatal(err)
	}
	var check proto.LauncherCheck
	if err := json.Unmarshal(res, &check); err != nil || !check.OK || check.Model != "opus" {
		t.Fatalf("model override did not reach the probe: %s, error=%v", res, err)
	}
}

func TestLauncherCheckRejectsInvalidModel(t *testing.T) {
	n := &Node{}
	res := n.launcherCheck(context.Background(), "claude", "opus; touch /tmp/invalid-model")
	if res.OK || res.Error != "invalid model" {
		t.Fatalf("invalid probe model accepted: %+v", res)
	}
}
