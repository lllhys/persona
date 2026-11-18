package node

// The preview.* methods (Node.handle, #8089); see handle_file.go. None reads the shared request fields: each RPC decodes
// its own request (preview_launch.go, preview.go).
const (
	methodPreviewServers = "preview.servers"
	methodPreviewStart   = "preview.start"
	methodPreviewOpen    = "preview.open"
)
