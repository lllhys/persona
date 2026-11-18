package hub

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/nwverse/persona/server/internal/proto"
)

func codexInventoryKey(machine string) string { return "codex_inventory/" + machine }

func (m *Machine) codexConfig(ctx context.Context) proto.CodexConfig {
	var cfg proto.CodexConfig
	if m.hub.store != nil {
		_, _ = m.hub.store.Setting(ctx, codexInventoryKey(m.cfg.ID), &cfg)
	}
	if m.cfg.CodexDiscovery != nil {
		cfg.Discovery = m.cfg.CodexDiscovery
	}
	for _, h := range m.cfg.CodexHomes {
		if !slices.Contains(cfg.Homes, h) {
			cfg.Homes = append(cfg.Homes, h)
		}
	}
	for _, s := range m.cfg.CodexServers {
		i := slices.IndexFunc(cfg.Servers, func(v proto.CodexServer) bool { return v.ID == s.ID })
		if i >= 0 {
			cfg.Servers[i] = s
		} else {
			cfg.Servers = append(cfg.Servers, s)
		}
	}
	return cfg
}

// codexInventoryHanded: the node's current connection, as its manager reports it, is the one the hub handed the kept
// inventory to (devices.connected, machineConnected's codex). A node's registry starts empty on every connection and
// gets the kept inventory only then; a report from before (a node before this check waited for it, node
// codex_registry.go rememberCodex) would store that empty registry over what the hub kept: the threads the user joined,
// the other homes and the app-servers. A machine the hub connects itself (no manager reports it) is taken as handed.
func (m *Machine) codexInventoryHanded() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.managed || m.connected && !m.hookedAt.IsZero() && m.hookedAt.Equal(m.connectedAt)
}

func (m *Machine) rememberCodexInventory(raw json.RawMessage) {
	if m.hub.store == nil {
		return
	}
	var cfg proto.CodexConfig
	if json.Unmarshal(raw, &cfg) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5e9)
	defer cancel()
	// Updates come from this authenticated node, not public endpoint URLs. The node never reports credentials.
	if err := m.hub.store.PutSetting(ctx, codexInventoryKey(m.cfg.ID), cfg); err != nil {
		m.hub.logf("[%s] remember Codex inventory: %v", m.cfg.ID, err)
	}
}
