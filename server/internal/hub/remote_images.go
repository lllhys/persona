package hub

import "github.com/nwverse/persona/server/internal/assistantwire"

// featureRemoteImages: the default manager answers GET /api/v1/remote-image, the pictures a reply names by address
// fetched, checked and scaled for the apps (#9473, docs/protocol/protocol.md "Remote images"). The route goes to it, so
// its hello alone decides; an extension's report cannot add the feature (features).
const featureRemoteImages = assistantwire.CapabilityRemoteImages

// remoteImagesFeatures is the feature while the default manager is connected and named the capability in its hello;
// nothing otherwise, so the apps keep loading such pictures as before with an older manager.
func (h *Hub) remoteImagesFeatures() []string {
	if !h.defaultManagerNames(assistantwire.CapabilityRemoteImages) {
		return nil
	}
	return []string{featureRemoteImages}
}
