package assistantwire

// CapabilityRemoteImages: the manager answers RemoteImageRoute, the pictures a reply names by address fetched, checked,
// scaled and kept by the hub's side for the apps (#9473, docs/protocol/protocol.md "Remote images"). The hub lists the
// remote_images feature only while the default manager names it; without it the apps keep loading such pictures as
// before (iOS straight from their host, the web behind a tap).
const CapabilityRemoteImages = "remote_images"

// RemoteImageRoute is GET /api/v1/remote-image?url=<absolute http(s) address>&size=<long side in px>: full
// credentials only (no extension scope opens it).
const RemoteImageRoute = "GET /api/v1/remote-image"

func init() { Routes = append(Routes, RemoteImageRoute) }
