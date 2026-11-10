package extension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTunnelContribution(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "extension", "examples", "Manifest.tunnel.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Tunnels(); len(got) != 1 || got[0].ID != "office" || got[0].Name != "公司内网隧道" {
		t.Fatalf("%+v", got)
	}
	if got := ContributionTitle(m, "tunnels", "office"); got != "公司内网隧道" {
		t.Fatalf("title %q", got)
	}
}

func TestTunnelNeedsATarget(t *testing.T) {
	manifest := func(config string) string {
		return `{"manifest_version":1,"id":"t","name":"T","version":"1.0.0",
			"contributes":{"tunnels":[{"id":"office","name":"Office"}]}` + config + `}`
	}
	for name, tc := range map[string]struct{ config, want string }{
		"no configuration":        {"", "needs target_host"},
		"host not required":       {`,"configuration":{"type":"object","properties":{"target_host":{"type":"string"}}}`, "needs target_host"},
		"host not a string":       {`,"configuration":{"type":"object","properties":{"target_host":{"type":"integer"}},"required":["target_host"]}`, "needs target_host"},
		"port not an integer":     {`,"configuration":{"type":"object","properties":{"target_host":{"type":"string"},"target_port":{"type":"string"}},"required":["target_host"]}`, "target_port must be an integer"},
		"ssh port not an integer": {`,"configuration":{"type":"object","properties":{"target_host":{"type":"string"},"ssh_port":{"type":"string"}},"required":["target_host"]}`, "ssh_port must be an integer"},
	} {
		_, err := Parse([]byte(manifest(tc.config)))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok := manifest(`,"configuration":{"type":"object","properties":{"target_host":{"type":"string"}},"required":["target_host"]}`)
	if _, err := Parse([]byte(ok)); err != nil {
		t.Fatal("host only:", err)
	}
	bad := `{"manifest_version":1,"id":"t","name":"T","version":"1.0.0","contributes":{"tunnels":[{"id":"office","name":""}]},
		"configuration":{"type":"object","properties":{"target_host":{"type":"string"}},"required":["target_host"]}}`
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("an empty name is refused")
	}
}
