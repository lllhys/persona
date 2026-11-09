package hub

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// bands is a w×h picture of three vertical bands: red on the left, green in the middle, blue on the right, each side
// band side wide. Its centered square is all green when the side bands are what makes it wider than tall.
func bands(w, h, side int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{0, 200, 0, 255}
			if x < side {
				c = color.RGBA{220, 0, 0, 255}
			} else if x >= w-side {
				c = color.RGBA{0, 0, 220, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func pngBase64(t *testing.T, img image.Image) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func uploadBody(t *testing.T, v map[string]any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// broadcastTypes drains what the app was sent, counting each type.
func broadcastTypes(c *client) map[string]int {
	n := map[string]int{}
	for {
		select {
		case b := <-c.out:
			var m struct{ Type string }
			if json.Unmarshal(b, &m) == nil {
				n[m.Type]++
			}
		default:
			return n
		}
	}
}

func nrgba(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

// An image uploaded from an app becomes a look of the hub's own: a package every app lists (the old list too), chosen
// when asked, with every device told; deleted again, the default comes back. Only these packages can be deleted.
func TestLookUploads(t *testing.T) {
	h := newTestHub(t)
	h.store = &settingsStore{vals: map[string][]byte{}}
	mux := h.routes()
	master := h.cfg.Token
	app := appClient(h)

	if !slices.Contains(proto.Features, "look_uploads") {
		t.Fatal("the feature is not announced")
	}

	// 300×200 with 50 px bands at the sides: the centered square is the green middle.
	img := pngBase64(t, bands(300, 200, 50))
	code, out := call(t, mux, "POST", "/api/v1/looks/uploads", master, uploadBody(t, map[string]any{
		"name": "  Doudou  ", "subtitle": "A round little fellow", "image": img, "choose": true}))
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	look := out["look"].(map[string]any)
	id, _ := look["id"].(string)
	if !strings.HasPrefix(id, "u-") || look["name"] != "Doudou" || look["subtitle"] != "A round little fellow" ||
		look["source"] != "hub" || look["uploaded"] != true || look["app_icon"] != "persona" || look["rev"] == "" {
		t.Fatalf("the new look: %v", look)
	}
	if cur := out["current"].(map[string]any); cur["id"] != id || cur["effective"] != id {
		t.Fatalf("chosen: %v", out["current"])
	}
	if got := broadcastTypes(app); got["personas"] == 0 || got["look"] != 1 {
		t.Fatalf("the apps are not told: %v", got)
	}

	_, out = call(t, mux, "GET", "/api/v1/looks", master, "")
	listed := lookByID(t, out, id)
	if listed == nil || listed["uploaded"] != true || out["current"].(map[string]any)["effective"] != id {
		t.Fatalf("listed: %v", out)
	}
	if persona := lookByID(t, out, "persona"); persona["uploaded"] != nil {
		t.Fatalf("a built-in look is not an uploaded one: %v", persona)
	}
	var m struct {
		Format int    `json:"format"`
		ID     string `json:"id"`
		Avatar struct {
			Sizes map[string]string `json:"sizes"`
		} `json:"avatar"`
		Dot struct {
			Size   int                              `json:"size"`
			States map[string]struct{ File string } `json:"states"`
		} `json:"dot"`
		Source struct{ Kind string } `json:"source"`
	}
	raw, _ := json.Marshal(listed["manifest"])
	if err := json.Unmarshal(raw, &m); err != nil || m.Format != 1 || m.ID != id || len(m.Avatar.Sizes) != 4 || m.Dot.Size != 144 ||
		len(m.Dot.States) != 5 || m.Source.Kind != "upload" {
		t.Fatalf("manifest: %s", raw)
	}

	dir := filepath.Join(h.personasDir(), id)
	big := readPNG(t, filepath.Join(dir, "avatar", "512.png"))
	if b := big.Bounds(); b.Dx() != 512 || b.Dy() != 512 {
		t.Fatalf("avatar 512: %v", b)
	}
	for _, p := range []image.Point{{2, 256}, {256, 256}, {509, 256}} { // the square is the middle: no red, no blue
		if c := nrgba(big, p.X, p.Y); c.G < 150 || c.R > 60 || c.B > 60 {
			t.Fatalf("avatar at %v is %v: not the centered square", p, c)
		}
	}
	idle := readPNG(t, filepath.Join(dir, "dot", "idle.png"))
	if c := nrgba(idle, 72, 72); c.G < 150 || c.A != 255 {
		t.Fatalf("idle's middle: %v", c)
	}
	if c := nrgba(idle, 1, 1); c.A != 0 {
		t.Fatalf("idle's corner is not clear: %v", c)
	}
	needs := readPNG(t, filepath.Join(dir, "dot", "needs_you.png"))
	if c := nrgba(needs, 72, 4); c.R < 180 || c.G > 120 { // the ring on the top edge
		t.Fatalf("needs you's ring: %v", c)
	}
	working := readPNG(t, filepath.Join(dir, "dot", "working.png"))
	if c := nrgba(working, 140, 72); c.R < 180 || c.B > 120 { // the arc at three o'clock (it runs from twelve)
		t.Fatalf("working's arc: %v", c)
	}
	if c := nrgba(working, 4, 72); c.R > 150 { // and not at nine o'clock: there only the faint ring
		t.Fatalf("working's arc goes round: %v", c)
	}
	offline := readPNG(t, filepath.Join(dir, "dot", "offline.png"))
	if c := nrgba(offline, 72, 100); int(c.G)-int(c.R) > 60 {
		t.Fatalf("offline is not grayed: %v", c)
	}
	thinking := readPNG(t, filepath.Join(dir, "dot", "thinking.png"))
	if c := nrgba(thinking, 115, 22); c.R < 230 || c.G < 230 || c.B < 230 { // inside the bubble, between its dots
		t.Fatalf("thinking's bubble: %v", c)
	}

	// The list the older apps read has it too.
	_, out = call(t, mux, "GET", "/api/v1/personas", master, "")
	found := false
	for _, p := range out["personas"].([]any) {
		found = found || p.(map[string]any)["id"] == id
	}
	if !found {
		t.Fatalf("not in /personas: %v", out)
	}

	// Without choose: made, not chosen.
	code, out = call(t, mux, "POST", "/api/v1/looks/uploads", master, uploadBody(t, map[string]any{"name": "Second", "image": img}))
	if code != 200 || out["current"] != nil {
		t.Fatalf("%d %v", code, out)
	}
	second := out["look"].(map[string]any)["id"].(string)
	_ = broadcastTypes(app)

	// A package placed by hand is not one to delete here.
	hand := filepath.Join(h.personasDir(), "doudou")
	if err := os.CopyFS(hand, os.DirFS(filepath.Join(h.personasDir(), second))); err != nil {
		t.Fatal(err)
	}
	manifest, _ := os.ReadFile(filepath.Join(hand, "persona.json"))
	manifest = bytes.Replace(manifest, []byte(`"`+second+`"`), []byte(`"doudou"`), 1)
	manifest = bytes.Replace(manifest, []byte(`"kind": "upload"`), []byte(`"kind": "made-by-hand"`), 1)
	if err := os.WriteFile(filepath.Join(hand, "persona.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := call(t, mux, "DELETE", "/api/v1/looks/uploads/doudou", master, ""); code != 409 || out["code"] != "not_uploaded" {
		t.Fatalf("a hand-placed package: %d %v", code, out)
	}
	if _, err := os.Stat(hand); err != nil {
		t.Fatalf("the hand-placed package went: %v", err)
	}
	for _, bad := range []string{"u-none", "persona", "..%2Fx"} {
		if code, _ := call(t, mux, "DELETE", "/api/v1/looks/uploads/"+bad, master, ""); code != 404 {
			t.Fatalf("delete %s: %d", bad, code)
		}
	}

	// Deleting the chosen one brings the default back, everywhere.
	code, out = call(t, mux, "DELETE", "/api/v1/looks/uploads/"+id, master, "")
	if cur, _ := out["current"].(map[string]any); code != 200 || cur["id"] != "persona" || cur["effective"] != "persona" {
		t.Fatalf("%d %v", code, out)
	}
	if got := broadcastTypes(app); got["look"] != 1 || got["personas"] == 0 {
		t.Fatalf("the apps are not told the default is back: %v", got)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("still on disk: %v", err)
	}
	entries, _ := os.ReadDir(h.personasDir())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Fatalf("left behind: %s", e.Name())
		}
	}
	_, out = call(t, mux, "GET", "/api/v1/looks", master, "")
	if lookByID(t, out, id) != nil || lookByID(t, out, second) == nil {
		t.Fatalf("after the delete: %v", out)
	}
	// Deleting one that is not chosen leaves the choice.
	if code, out := call(t, mux, "DELETE", "/api/v1/looks/uploads/"+second, master, ""); code != 200 || out["current"].(map[string]any)["id"] != "persona" {
		t.Fatalf("%d %v", code, out)
	}
	if got := broadcastTypes(app); got["look"] != 0 || got["personas"] == 0 {
		t.Fatalf("the choice did not change: no look broadcast, the list did: %v", got)
	}
}

// What an upload is refused for, each with its code and the user's language.
func TestLookUploadRefusals(t *testing.T) {
	h := newTestHub(t)
	h.store = &settingsStore{vals: map[string][]byte{}}
	mux := h.routes()
	master := h.cfg.Token
	img := pngBase64(t, bands(120, 120, 10))
	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, bands(160, 160, 20), nil); err != nil {
		t.Fatal(err)
	}
	tiny := pngBase64(t, bands(32, 32, 4))

	for _, tt := range []struct {
		name string
		body map[string]any
		code int
		want string
	}{
		{"no name", map[string]any{"name": "  ", "image": img}, 400, "no_name"},
		{"long name", map[string]any{"name": strings.Repeat("名", 41), "image": img}, 400, "bad_name"},
		{"control character", map[string]any{"name": "a\x07b", "image": img}, 400, "bad_name"},
		{"bidi override", map[string]any{"name": "a‮b", "image": img}, 400, "bad_name"},
		{"long subtitle", map[string]any{"name": "a", "subtitle": strings.Repeat("x", 81), "image": img}, 400, "bad_subtitle"},
		{"no image", map[string]any{"name": "a"}, 400, "no_image"},
		{"not base64", map[string]any{"name": "a", "image": "%%%"}, 400, "not_image"},
		{"not an image", map[string]any{"name": "a", "image": base64.StdEncoding.EncodeToString([]byte("hello, not a picture"))}, 400, "not_image"},
		{"too small", map[string]any{"name": "a", "image": tiny}, 400, "too_small"},
		{"jpeg as a data URL", map[string]any{"name": "a", "image": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpegBuf.Bytes())}, 200, ""},
		{"unpadded base64", map[string]any{"name": "a", "image": strings.TrimRight(img, "=")}, 200, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, out := call(t, mux, "POST", "/api/v1/looks/uploads", master, uploadBody(t, tt.body))
			if code != tt.code || (tt.want != "" && out["code"] != tt.want) {
				t.Fatalf("%d %v, want %d %s", code, out, tt.code, tt.want)
			}
			if tt.want != "" && (out["error"] == "" || out["error"] == "hub-look_uploads."+tt.want) {
				t.Fatalf("no message for %s: %v", tt.want, out)
			}
		})
	}

	// A body over the bound: 413 before anything is decoded.
	huge := `{"name":"a","image":"` + strings.Repeat("A", lookUploadMaxBody) + `"}`
	if code, out := call(t, mux, "POST", "/api/v1/looks/uploads", master, huge); code != 413 || out["code"] != "too_large" {
		t.Fatalf("huge: %d %v", code, out)
	}

	// At most lookUploadMaxLooks uploaded looks (hand-placed ones do not count).
	for i := range lookUploadMaxLooks {
		d := filepath.Join(h.personasDir(), fmt.Sprintf("u-fill%02d", i))
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "persona.json"), []byte(`{"source":{"kind":"upload"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if code, out := call(t, mux, "POST", "/api/v1/looks/uploads", master, uploadBody(t, map[string]any{"name": "one more", "image": img})); code != 409 || out["code"] != "too_many" {
		t.Fatalf("over the limit: %d %v", code, out)
	}
}
