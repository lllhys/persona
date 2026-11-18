package node

// A Mac's displays for the live screen (docs/extensions/computer-use.md "Displays"): macOS's Screen Sharing sends every
// display in one picture, laid out as System Settings › Displays arranges them, and the app crops it to one display
// with these frames.

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os/exec"
	"runtime"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// displaysScript prints AppKit's screens as JSON: macOS's display number, the frame in points, in AppKit's
// coordinates (origin at the bottom-left corner of the main display, y up), and the pixels per point (#9314). The
// first screen is the main one.
const displaysScript = `ObjC.import("AppKit");
JSON.stringify(ObjC.unwrap($.NSScreen.screens).map(s => ({id: ObjC.unwrap(s.deviceDescription.objectForKey("NSScreenNumber")), frame: s.frame, scale: s.backingScaleFactor})))`

// readDisplays asks AppKit for this Mac's displays (about 0.2 s). Nil off a Mac, without a window server session
// (logged out, a node started as a daemon) or when osascript fails: the app then offers no display choice.
var readDisplays = func(ctx context.Context) []proto.Display {
	if runtime.GOOS != "darwin" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", displaysScript).Output()
	if err != nil {
		return nil
	}
	return parseDisplays(out)
}

// screenDisplays: the displays, read only while Screen Sharing answers (nothing shows them otherwise).
func screenDisplays(ctx context.Context, sharing bool) []proto.Display {
	if !sharing {
		return nil
	}
	return readDisplays(ctx)
}

// parseDisplays turns displaysScript's output into frames in the global display space: origin at the main display's
// top-left corner, y down. AppKit's y is flipped about the main display's height.
func parseDisplays(out []byte) []proto.Display {
	var screens []struct {
		ID    int     `json:"id"`
		Scale float64 `json:"scale"`
		Frame struct {
			Origin struct {
				X float64 `json:"x"`
				Y float64 `json:"y"`
			} `json:"origin"`
			Size struct {
				Width  float64 `json:"width"`
				Height float64 `json:"height"`
			} `json:"size"`
		} `json:"frame"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &screens) != nil || len(screens) == 0 {
		return nil
	}
	mainHeight := screens[0].Frame.Size.Height
	var displays []proto.Display
	for i, s := range screens {
		f := s.Frame
		if f.Size.Width <= 0 || f.Size.Height <= 0 {
			continue
		}
		displays = append(displays, proto.Display{
			ID:     s.ID,
			X:      int(math.Round(f.Origin.X)),
			Y:      int(math.Round(mainHeight - f.Origin.Y - f.Size.Height)),
			Width:  int(math.Round(f.Size.Width)),
			Height: int(math.Round(f.Size.Height)),
			Main:   i == 0,
			Scale:  displayScale(s.Scale),
		})
	}
	return displays
}

// displayScale is a display's pixels per point as the phone gets it: 0 (left out) when AppKit gave none, else rounded to
// a quarter (1, 2, or a scaled mode's fraction).
func displayScale(v float64) float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*4) / 4
}
