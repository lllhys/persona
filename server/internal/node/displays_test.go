package node

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// AppKit's frames (origin bottom-left, y up) become the global display space (origin at the main display's top-left,
// y down): a laptop to the left of a larger main display, top edges level, and a third display above the main one.
func TestParseDisplaysFlipsAppKitFrames(t *testing.T) {
	out := []byte(`[{"id":3,"frame":{"origin":{"x":0,"y":0},"size":{"width":2560,"height":1440}}},` +
		`{"id":1,"frame":{"origin":{"x":-1728,"y":323},"size":{"width":1728,"height":1117}}},` +
		`{"id":5,"frame":{"origin":{"x":320,"y":1440},"size":{"width":1920,"height":1080}}}]` + "\n")
	want := []proto.Display{
		{ID: 3, X: 0, Y: 0, Width: 2560, Height: 1440, Main: true},
		{ID: 1, X: -1728, Y: 0, Width: 1728, Height: 1117},
		{ID: 5, X: 320, Y: -1080, Width: 1920, Height: 1080},
	}
	if got := parseDisplays(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// Anything else osascript may print (an error, no screens, a screen with no size) gives no displays rather than wrong ones.
func TestParseDisplaysRefusesWhatItCannotRead(t *testing.T) {
	for _, out := range []string{"", "[]", "execution error: Error: ReferenceError (-2700)", `{"id":1}`} {
		if got := parseDisplays([]byte(out)); got != nil {
			t.Errorf("%q: %+v", out, got)
		}
	}
	got := parseDisplays([]byte(`[{"id":1,"frame":{"origin":{"x":0,"y":0},"size":{"width":1512,"height":982}}},{"id":2,"frame":{"origin":{"x":0,"y":0},"size":{"width":0,"height":0}}}]`))
	if len(got) != 1 || got[0].ID != 1 || !got[0].Main {
		t.Fatalf("an empty screen is left out: %+v", got)
	}
}

// Displays are read only while Screen Sharing answers.
// Each display's pixels per point (#9314) comes through, rounded to a quarter; one AppKit left out stays 0 (an old
// node's displays have none either). The script asks AppKit for it.
func TestParseDisplaysKeepsTheScale(t *testing.T) {
	out := []byte(`[{"id":1,"scale":1,"frame":{"origin":{"x":0,"y":0},"size":{"width":1920,"height":1080}}},` +
		`{"id":23,"scale":2,"frame":{"origin":{"x":1920,"y":59},"size":{"width":540,"height":1021}}},` +
		`{"id":4,"scale":1.4999,"frame":{"origin":{"x":-800,"y":0},"size":{"width":800,"height":600}}},` +
		`{"id":5,"frame":{"origin":{"x":0,"y":1080},"size":{"width":800,"height":600}}}]`)
	got := parseDisplays(out)
	if len(got) != 4 || got[0].Scale != 1 || got[1].Scale != 2 || got[1].Y != 0 || got[2].Scale != 1.5 || got[3].Scale != 0 {
		t.Fatalf("got %+v", got)
	}
	if !strings.Contains(displaysScript, "backingScaleFactor") {
		t.Fatal("the script does not read the scale")
	}
}

func TestScreenDisplaysOnlyWithScreenSharing(t *testing.T) {
	asked := 0
	old := readDisplays
	readDisplays = func(context.Context) []proto.Display { asked++; return []proto.Display{{ID: 1, Width: 1, Height: 1}} }
	t.Cleanup(func() { readDisplays = old })
	if screenDisplays(context.Background(), false) != nil || asked != 0 {
		t.Fatalf("read without Screen Sharing: %d", asked)
	}
	if len(screenDisplays(context.Background(), true)) != 1 || asked != 1 {
		t.Fatalf("not read with Screen Sharing: %d", asked)
	}
}
