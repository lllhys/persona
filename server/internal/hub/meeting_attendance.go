package hub

import (
	"context"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// featureMeetingAttendance: the default manager attends recorded meetings (docs/assistant/meeting-minutes.md
// "Attending a meeting"). The recordings' routes go to it, so its hello alone decides; an extension's report cannot
// add the feature (features).
const featureMeetingAttendance = assistantwire.CapabilityMeetingAttendance

// meetingAttendanceFeatures is the feature while the default manager is connected and named the capability in its
// hello; nothing otherwise, so a phone offers no role to an older manager, which would ignore it.
func (h *Hub) meetingAttendanceFeatures() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if h.archiveManagerHas(h.defaultOwner(ctx), assistantwire.CapabilityMeetingAttendance) != http.StatusOK {
		return nil
	}
	return []string{featureMeetingAttendance}
}
