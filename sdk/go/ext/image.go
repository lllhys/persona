package ext

import (
	"encoding/base64"
	"net/http"
	"os"

	"github.com/nwverse/persona/sdk/go/wire"
)

// Image is the answer to ui.image: the picture's bytes (a png, jpeg, webp or gif, at most 4 MB and 8192 pixels a
// side), and how long the hub may keep it in seconds (a camera 5 to 10, a map 30, a still 86400; 0 not at all).
// The hub reads the type from the bytes, so mime is only a hint; empty, it is read here the same way.
func Image(mime string, data []byte, maxAge int) *wire.UIImage {
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	age := min(max(maxAge, 0), wire.ImageMaxAgeMost)
	return &wire.UIImage{Mime: mime, Data: base64.StdEncoding.EncodeToString(data), MaxAge: &age}
}

// ImageFile is Image of a file the extension ships (its package directory: Env().Dir), the type read from its bytes.
func ImageFile(path string, maxAge int) (*wire.UIImage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Image("", data, maxAge), nil
}

// ToolImage is one picture of a connector's tools.call answer (wire.ConnectorCallResult.Images, #8484), for the model
// after the text, on a hub whose initialize lists wire.HubCapabilityToolImages (Init.Info().Hub.Capabilities); an
// older hub ignores it, so the text must stand on its own. data is a whole JPEG, PNG or WebP of at most 1 MiB and
// 2000 pixels a side; caption is one short line said before it (a time in a video). The hub leaves out a picture over
// a limit, and one over the answer's 16 pictures, 4 MiB or 8 million pixels in all, and says so after the text.
func ToolImage(data []byte, caption string) wire.ConnectorToolImage {
	return wire.ConnectorToolImage{Mime: http.DetectContentType(data), Data: base64.StdEncoding.EncodeToString(data), Caption: caption}
}
