package transcript

import "testing"

func TestObserveCodexEffortMetadata(t *testing.T) {
	var m Meta
	m.ObserveNamingMetadata([]byte(`{"timestamp":"2026-11-13T12:01:00Z","type":"turn_context","payload":{"effort":"max"}}`))
	m.ObserveNamingMetadata([]byte(`{"timestamp":"2026-11-13T12:00:00Z","type":"turn_context","payload":{"effort":"low"}}`))
	m.ObserveNamingMetadata([]byte(`{"timestamp":"2026-11-13T12:02:00Z","type":"turn_context","payload":{}}`))
	m.ObserveNamingMetadata([]byte(`{"timestamp":"2026-11-13T12:03:00Z","type":"event_msg","payload":{"type":"token_count"},"note":{"type":"turn_context","effort":"low"}}`))
	if m.Effort != "max" || m.EffortAt.IsZero() {
		t.Fatalf("small metadata observation lost the latest recorded effort: %q at %v", m.Effort, m.EffortAt)
	}
}
