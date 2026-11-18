package node

// The clipboard.* methods (Node.handle, #8089); see handle_file.go. Neither reads the shared request fields:
// clipboard.set decodes its own request in clipshare.
const (
	methodClipboardGet = "clipboard.get"
	methodClipboardSet = "clipboard.set"
)
