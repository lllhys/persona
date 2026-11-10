package devices

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/nodeproto"
)

func TestConfigureNodeForwardsHubFeatures(t *testing.T) {
	_, _, m, _ := dirsHub(t)
	f := connectDirsNode(t, m)
	features := []string{nodeproto.PersonaContracts, "future_feature"}
	raw, _ := json.Marshal(features)
	for _, tt := range []struct {
		name  string
		reply *devicewire.ConnectedReply
		want  []string
	}{
		{"current hub", &devicewire.ConnectedReply{Configure: map[string]json.RawMessage{"hub_features": raw}}, features},
		{"old hub reconnect", &devicewire.ConnectedReply{}, []string{}},
		{"hub unavailable", nil, []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m.configureNode(context.Background(), m.peer, tt.reply, true)
			f.mu.Lock()
			last := f.configure[len(f.configure)-1]
			f.mu.Unlock()
			var got []string
			if err := json.Unmarshal(last["hub_features"], &got); err != nil {
				t.Fatalf("hub_features missing or invalid: %s (%v)", last["hub_features"], err)
			}
			if got == nil || !slices.Equal(got, tt.want) {
				t.Fatalf("hub_features = %v, want %v", got, tt.want)
			}
		})
	}
}
