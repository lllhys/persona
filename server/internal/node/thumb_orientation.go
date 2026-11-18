package node

import (
	"io"

	"github.com/nwverse/persona/server/internal/imageorient"
)

// jpegThumbOrientation reads headers only, stopping before compressed pixels; the complete scan shares the thumbnail
// source-size bound. Bad metadata means identity (imageorient.JPEG).
func jpegThumbOrientation(src io.Reader) int { return imageorient.JPEG(src, thumbMaxSource) }

// orientedThumb presents display coordinates to the scaler without allocating another full-size image.
type orientedThumb = imageorient.Image
