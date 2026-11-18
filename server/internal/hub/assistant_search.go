package hub

import "github.com/nwverse/persona/server/internal/assistantwire"

// featureAssistantSearch: the default manager answers GET /api/v1/assistant/search, the chats found by what was said in
// them (#6282, docs/protocol/protocol.md "Assistant chat search"). The route goes to it, so its hello alone decides, as
// for recording_day; an extension's report cannot add the feature (features).
const featureAssistantSearch = assistantwire.CapabilityAssistantSearch

// assistantSearchFeatures is the feature while the default manager is connected and named the capability in its hello;
// nothing otherwise, so the apps keep filtering titles and last messages with an older manager.
func (h *Hub) assistantSearchFeatures() []string {
	if !h.defaultManagerNames(assistantwire.CapabilityAssistantSearch) {
		return nil
	}
	return []string{featureAssistantSearch}
}
