package hub

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // the first frame of a GIF
	_ "image/jpeg"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
)

// Uploaded looks (docs/apple/persona.md "Uploading an image"): the user makes Persona's avatar from an image of their own
// in the apps. The hub turns the image into a persona package of its own (format 1) in personas/u-<hex>/, so every app
// that shows packages shows it, and deletes it again on request. Only packages it made this way can be deleted here:
// one placed by hand, a built-in one and an extension's are left alone.
//
// The apps crop the image square and send it upright; the hub still crops a non-square one to its centered square. The
// package has the avatar at the sizes the apps pick from and a still frame for each dot state, marked the way the
// generated packages mark them (a thought bubble, an arc on the rim, the red ring, gray and asleep), so the top bar
// still says what Persona is doing; a still frame is what Reduce Motion shows of any package anyway.

const (
	lookUploadKind      = "upload" // persona.json's source.kind
	lookUploadIDPrefix  = "u-"
	lookUploadMaxBody   = 12 << 20 // the JSON body: an 8 MB image in base64 and the rest
	lookUploadMaxImage  = 8 << 20
	lookUploadMaxPixels = 40_000_000 // decoded RGBA stays under 160 MB
	lookUploadMinSide   = 64
	lookUploadMaxLooks  = 50
	lookUploadMaxName   = 40
	lookUploadMaxSub    = 80
	lookUploadDotSide   = 144 // the top bar shows the dot at 46 pt: 144 px is 48 pt @3x, as the generated packages
	lookUploadSuper     = 3   // the dot frames are drawn at 3× and scaled down, for smooth edges
)

// lookUploadSizes are the avatar's sides; the apps pick the smallest one big enough for what they draw.
var lookUploadSizes = []int{64, 128, 256, 512}

// lookUploadsMu keeps two uploads (or an upload and a delete) from counting or writing the directory at once.
var lookUploadsMu sync.Mutex

var (
	lookUploadAccent   = color.NRGBA{0xF0, 0x8C, 0x1E, 0xFF} // the working arc and the lit thinking dot: Mochi's orange
	lookUploadNeedsYou = color.NRGBA{0xE5, 0x48, 0x4D, 0xFF} // make_persona.py's NEEDS_YOU
	lookUploadDim      = color.NRGBA{150, 150, 158, 0xFF}    // the thinking dots not lit
	lookUploadZ        = color.NRGBA{72, 72, 84, 0xFF}       // the offline z's
	lookUploadWhite    = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
)

// lookUploadRequest is POST /api/v1/looks/uploads's body.
type lookUploadRequest struct {
	Name     string `json:"name"`
	Subtitle string `json:"subtitle"`
	Image    string `json:"image"` // base64 (standard, padding optional) of a PNG, JPEG, WebP or GIF
	Choose   bool   `json:"choose"`
}

// lookUploadError is a refusal to tell the user: its status and stable code, with the message in their language.
type lookUploadError struct {
	status int
	code   string
	err    error
}

func (e *lookUploadError) Error() string { return e.err.Error() }

func lookUploadRefusal(status int, code string, args ...i18n.Args) *lookUploadError {
	return &lookUploadError{status: status, code: code, err: i18n.Err("hub-look_uploads."+code, args...)}
}

func (h *Hub) lookUploadRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/looks/uploads", func(w http.ResponseWriter, r *http.Request) {
		var body lookUploadRequest
		if err := decodeJSONBody(w, r, &body, lookUploadMaxBody); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeLookUploadErr(w, lookUploadRefusal(http.StatusRequestEntityTooLarge, "too_large", i18n.Args{"mb": lookUploadMaxImage >> 20}))
				return
			}
			writeBodyErr(w, err, err)
			return
		}
		view, cur, err := h.uploadLook(r.Context(), body)
		if err != nil {
			writeLookUploadErr(w, err)
			return
		}
		out := map[string]any{"look": view}
		if cur != nil {
			out["current"] = cur
		}
		writeJSON(w, out)
	})
	api.HandleFunc("DELETE /api/v1/looks/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		cur, err := h.deleteUploadedLook(r.Context(), r.PathValue("id"))
		if err != nil {
			writeLookUploadErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"current": cur})
	})
}

func writeLookUploadErr(w http.ResponseWriter, err error) {
	var refusal *lookUploadError
	if errors.As(err, &refusal) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(refusal.status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": refusal.err.Error(), "code": refusal.code})
		return
	}
	writeErr(w, http.StatusInternalServerError, err)
}

// uploadLook makes a package of the image, and chooses it when asked. It returns the new look as GET /api/v1/looks
// lists it, and the choice when it chose it.
func (h *Hub) uploadLook(ctx context.Context, req lookUploadRequest) (lookView, *lookCurrent, error) {
	name, subtitle, err := lookUploadNames(req.Name, req.Subtitle)
	if err != nil {
		return lookView{}, nil, err
	}
	img, err := decodeLookUpload(req.Image)
	if err != nil {
		return lookView{}, nil, err
	}

	lookUploadsMu.Lock()
	defer lookUploadsMu.Unlock()
	if n := h.uploadedLookCount(); n >= lookUploadMaxLooks {
		return lookView{}, nil, lookUploadRefusal(http.StatusConflict, "too_many", i18n.Args{"max": lookUploadMaxLooks})
	}
	id, err := h.newUploadID()
	if err != nil {
		return lookView{}, nil, err
	}
	files, err := lookUploadFiles(id, name, subtitle, img, time.Now().UTC().Truncate(time.Second))
	if err != nil {
		return lookView{}, nil, err
	}
	if err := h.writeUploadedLook(id, files); err != nil {
		return lookView{}, nil, err
	}
	h.logf("look %s uploaded (%s)", id, name)
	h.broadcast(map[string]any{"type": "personas"})

	looks := h.looks()
	var view lookView
	for _, v := range looks {
		if v.ID == id {
			view = v
		}
	}
	if view.ID == "" { // written but not listed: it would never show, so it is not kept either
		_ = os.RemoveAll(filepath.Join(h.personasDir(), id))
		return lookView{}, nil, fmt.Errorf("look %s was written but does not check out", id)
	}
	if !req.Choose || h.store == nil {
		return view, nil, nil
	}
	cur, err := h.chooseLook(ctx, id, looks)
	if err != nil {
		return view, nil, err
	}
	return view, &cur, nil
}

// deleteUploadedLook deletes a package the hub made from an upload. When it was the chosen look, the default is chosen.
func (h *Hub) deleteUploadedLook(ctx context.Context, id string) (lookCurrent, error) {
	if !personaIDPattern.MatchString(id) {
		return lookCurrent{}, lookUploadRefusal(http.StatusNotFound, "not_found")
	}
	lookUploadsMu.Lock()
	defer lookUploadsMu.Unlock()
	dir := filepath.Join(h.personasDir(), id)
	raw, err := os.ReadFile(filepath.Join(dir, "persona.json"))
	if errors.Is(err, os.ErrNotExist) {
		return lookCurrent{}, lookUploadRefusal(http.StatusNotFound, "not_found")
	} else if err != nil {
		return lookCurrent{}, err
	}
	if !isUploadedManifest(raw) {
		return lookCurrent{}, lookUploadRefusal(http.StatusConflict, "not_uploaded")
	}
	// Out of the list in one step (the hub skips dot directories), then off the disk.
	gone := filepath.Join(h.personasDir(), "."+id+".gone-"+randomHex(4))
	if err := os.Rename(dir, gone); err != nil {
		return lookCurrent{}, err
	}
	if err := os.RemoveAll(gone); err != nil {
		h.logf("look %s: remove %s: %v", id, gone, err)
	}
	h.logf("look %s deleted", id)
	h.broadcast(map[string]any{"type": "personas"})

	looks := h.looks()
	chosen, err := h.lookChoice(ctx)
	if err != nil {
		return lookCurrent{}, err
	}
	if chosen != nil && chosen.ID == id {
		return h.chooseLook(ctx, extension.DefaultLook, looks)
	}
	return lookCurrentOf(chosen, looks), nil
}

// lookUploadNames are the name and subtitle as the package keeps them: trimmed, the name required, both bounded and
// free of control and bidi formatting characters.
func lookUploadNames(name, subtitle string) (string, string, error) {
	name, subtitle = strings.TrimSpace(name), strings.TrimSpace(subtitle)
	if name == "" {
		return "", "", lookUploadRefusal(http.StatusBadRequest, "no_name")
	}
	if !lookUploadText(name) || utf8.RuneCountInString(name) > lookUploadMaxName {
		return "", "", lookUploadRefusal(http.StatusBadRequest, "bad_name", i18n.Args{"max": lookUploadMaxName})
	}
	if !lookUploadText(subtitle) || utf8.RuneCountInString(subtitle) > lookUploadMaxSub {
		return "", "", lookUploadRefusal(http.StatusBadRequest, "bad_subtitle", i18n.Args{"max": lookUploadMaxSub})
	}
	return name, subtitle, nil
}

func lookUploadText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return false
		}
	}
	return true
}

// decodeLookUpload decodes the image the request carries, bounded in bytes and pixels before it is decoded.
func decodeLookUpload(b64 string) (image.Image, error) {
	b64 = strings.TrimSpace(b64)
	if i := strings.Index(b64, ";base64,"); strings.HasPrefix(b64, "data:") && i >= 0 { // a data: URL is fine too
		b64 = b64[i+len(";base64,"):]
	}
	if b64 == "" {
		return nil, lookUploadRefusal(http.StatusBadRequest, "no_image")
	}
	if base64.StdEncoding.DecodedLen(len(b64)) > lookUploadMaxImage+3 {
		return nil, lookUploadRefusal(http.StatusRequestEntityTooLarge, "too_large", i18n.Args{"mb": lookUploadMaxImage >> 20})
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(b64, "="))
	}
	if err != nil {
		return nil, lookUploadRefusal(http.StatusBadRequest, "not_image")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, lookUploadRefusal(http.StatusBadRequest, "not_image")
	}
	if cfg.Width < lookUploadMinSide || cfg.Height < lookUploadMinSide {
		return nil, lookUploadRefusal(http.StatusBadRequest, "too_small", i18n.Args{"px": lookUploadMinSide})
	}
	if int64(cfg.Width)*int64(cfg.Height) > lookUploadMaxPixels {
		return nil, lookUploadRefusal(http.StatusRequestEntityTooLarge, "too_many_pixels")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, lookUploadRefusal(http.StatusBadRequest, "not_image")
	}
	return img, nil
}

// uploadedLookCount counts the packages in personas/ the hub made from uploads.
func (h *Hub) uploadedLookCount() int {
	entries, err := os.ReadDir(h.personasDir())
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), lookUploadIDPrefix) {
			continue
		}
		if raw, err := os.ReadFile(filepath.Join(h.personasDir(), e.Name(), "persona.json")); err == nil && isUploadedManifest(raw) {
			n++
		}
	}
	return n
}

// isUploadedManifest says whether a persona.json is one the hub wrote for an upload.
func isUploadedManifest(raw []byte) bool {
	var m struct {
		Source struct {
			Kind string `json:"kind"`
		} `json:"source"`
	}
	return json.Unmarshal(raw, &m) == nil && m.Source.Kind == lookUploadKind
}

func (h *Hub) newUploadID() (string, error) {
	for range 8 {
		id := lookUploadIDPrefix + randomHex(4)
		if _, err := os.Lstat(filepath.Join(h.personasDir(), id)); errors.Is(err, os.ErrNotExist) && !extension.ReservedLookID(id) {
			return id, nil
		}
	}
	return "", errors.New("no free look id")
}

// writeUploadedLook writes the package beside the others under a dot name (the hub skips those) and moves it into place
// in one step, so no list ever has half of it.
func (h *Hub) writeUploadedLook(id string, files map[string][]byte) error {
	root := h.personasDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(root, "."+id+".new-")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = os.RemoveAll(tmp)
		}
	}()
	for name, b := range files {
		p := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o600); err != nil {
			return err
		}
	}
	if _, err := openPersona(os.DirFS(tmp), id); err != nil {
		return fmt.Errorf("the package made from the upload does not check out: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(root, id)); err != nil {
		return err
	}
	done = true
	return nil
}

// lookUploadFiles is the package made from img: persona.json, the avatar at each size and the dot's still frames.
func lookUploadFiles(id, name, subtitle string, img image.Image, at time.Time) (map[string][]byte, error) {
	square := squareOf(img)
	files := map[string][]byte{}
	sizes := map[string]string{}
	for _, side := range lookUploadSizes {
		f := fmt.Sprintf("avatar/%d.png", side)
		b, err := encodePNG(scaled(square, side))
		if err != nil {
			return nil, err
		}
		files[f], sizes[fmt.Sprint(side)] = b, f
	}
	states := map[string]any{}
	for _, state := range personaStates {
		f := "dot/" + state + ".png"
		b, err := encodePNG(lookUploadDot(square, state))
		if err != nil {
			return nil, err
		}
		files[f], states[state] = b, map[string]any{"file": f}
	}
	m := map[string]any{
		"format": personaFormat, "id": id, "name": name,
		"avatar":            map[string]any{"sizes": sizes, "safe": 1},
		"dot":               map[string]any{"size": lookUploadDotSide, "states": states},
		"app_icon_fallback": extension.DefaultLook,
		"source":            map[string]any{"kind": lookUploadKind, "uploaded_at": at.Format(time.RFC3339)},
	}
	if subtitle != "" {
		m["subtitle"] = subtitle
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	files["persona.json"] = append(raw, '\n')
	return files, nil
}

// squareOf is img's centered square, as an RGBA image of its own.
func squareOf(img image.Image) *image.RGBA {
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	out := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(out, out.Bounds(), img, image.Pt(x0, y0), draw.Src)
	return out
}

func scaled(src image.Image, side int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.CatmullRom.Scale(out, out.Bounds(), src, src.Bounds(), draw.Src, nil)
	return out
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// lookUploadDot is one state's still frame: the image in a circle with the state's mark, drawn at lookUploadSuper times
// the size and scaled down. Frame 0 of a generated package says its state alone; so does this one.
func lookUploadDot(square *image.RGBA, state string) *image.RGBA {
	W := lookUploadDotSide * lookUploadSuper
	c := &canvas{RGBA: image.NewRGBA(image.Rect(0, 0, W, W)), w: float64(W)}
	photo := scaled(square, W)
	if state == "offline" {
		grayOut(photo)
	}
	c.circle(photo)
	c.ring(0.012, color.NRGBA{0xFF, 0xFF, 0xFF, 110}) // the white rim every package has
	switch state {
	case "thinking":
		c.thinking()
	case "working":
		c.ring(0.075, withAlpha(lookUploadAccent, 60))
		c.arc(0.075, -90, 20, lookUploadAccent) // frame 0 of the generated packages: from the top, a third of the way round
	case "needs_you":
		c.ring(0.07, lookUploadNeedsYou)
	case "offline":
		c.sleeping()
	}
	return scaled(c.RGBA, lookUploadDotSide)
}

// canvas draws the dot's marks on a W×W image: shapes are given in fractions of the side, colors blended over what is
// there (supersampling smooths their edges).
type canvas struct {
	*image.RGBA
	w float64
}

// circle puts photo inside the circle the frame is, leaving the corners clear.
func (c *canvas) circle(photo *image.RGBA) {
	r := c.w / 2
	for y := 0; y < c.Rect.Dy(); y++ {
		for x := 0; x < c.Rect.Dx(); x++ {
			if math.Hypot(float64(x)+0.5-r, float64(y)+0.5-r) <= r {
				c.SetRGBA(x, y, photo.RGBAAt(x, y))
			}
		}
	}
}

// ring is a ring of width wf (a fraction of the side) along the inside of the edge.
func (c *canvas) ring(wf float64, col color.NRGBA) {
	c.arc(wf, 0, 360, col)
}

// arc is the ring between from and to degrees (0 at three o'clock, clockwise, as PIL's arc), with round ends.
func (c *canvas) arc(wf, from, to float64, col color.NRGBA) {
	w := c.w * wf
	r := c.w/2 - w/2 // the middle of the stroke
	full := to-from >= 360
	cx := c.w / 2
	end := func(deg float64) (float64, float64) {
		a := deg * math.Pi / 180
		return cx + r*math.Cos(a), cx + r*math.Sin(a)
	}
	ax, ay := end(from)
	bx, by := end(to)
	c.each(func(x, y float64) bool {
		d := math.Abs(math.Hypot(x-cx, y-cx) - r)
		if d > w/2 {
			return false
		}
		if full {
			return true
		}
		deg := math.Mod(math.Atan2(y-cx, x-cx)*180/math.Pi-from+720, 360)
		return deg <= to-from || math.Hypot(x-ax, y-ay) <= w/2 || math.Hypot(x-bx, y-by) <= w/2
	}, col)
}

// thinking is the thought bubble at the top right with three dots, the first one lit (frame 0 of the generated packs).
func (c *canvas) thinking() {
	W := c.w
	cx, cy, r := W*0.76, W*0.19, W*0.15
	halfW, halfH := r, r*0.72
	bubble := func(x, y float64) bool { return inRoundRect(x, y, cx-halfW, cy-halfH, cx+halfW, cy+halfH, halfH) }
	tail := func(x, y float64) bool { return math.Hypot(x-(cx-r*0.7), y-(cy+r*0.77)) <= r*0.15 }
	// A soft outline first, so the white bubble stays apart from a light picture.
	outline := W * 0.012
	c.each(func(x, y float64) bool {
		return inRoundRect(x, y, cx-halfW-outline, cy-halfH-outline, cx+halfW+outline, cy+halfH+outline, halfH+outline) ||
			math.Hypot(x-(cx-r*0.7), y-(cy+r*0.77)) <= r*0.15+outline
	}, color.NRGBA{0, 0, 0, 40})
	c.each(func(x, y float64) bool { return bubble(x, y) || tail(x, y) }, color.NRGBA{0xFF, 0xFF, 0xFF, 250})
	for k := range 3 {
		x0 := cx + float64(k-1)*r*0.55
		rr, col := r*0.14, lookUploadDim
		if k == 0 {
			rr, col = r*0.17, lookUploadAccent
		}
		c.each(func(x, y float64) bool { return math.Hypot(x-x0, y-cy) <= rr }, col)
	}
}

// sleeping is three z's rising out of the top right, on a light halo so they read on a dark picture too.
func (c *canvas) sleeping() {
	W := c.w
	stroke := math.Max(2, W*0.026)
	// A still frame: the z's apart, growing and fading as they rise (the generated packages move them up instead).
	for k, z := range [3]struct{ x, y, size float64 }{{0.60, 0.40, 0.07}, {0.70, 0.27, 0.09}, {0.81, 0.11, 0.11}} {
		size := W * z.size
		x, y := W*z.x, W*z.y
		a := 255 - 40*k
		pts := [][2]float64{{x, y}, {x + size, y}, {x, y + size}, {x + size, y + size}}
		near := func(width float64) func(px, py float64) bool {
			return func(px, py float64) bool {
				for i := range 3 {
					if segmentDist(px, py, pts[i], pts[i+1]) <= width/2 {
						return true
					}
				}
				return false
			}
		}
		c.each(near(stroke*2), withAlpha(lookUploadWhite, uint8(a*3/5)))
		c.each(near(stroke), withAlpha(lookUploadZ, uint8(a)))
	}
}

// each blends col over every pixel whose center in is true for.
func (c *canvas) each(in func(x, y float64) bool, col color.NRGBA) {
	for y := 0; y < c.Rect.Dy(); y++ {
		for x := 0; x < c.Rect.Dx(); x++ {
			if in(float64(x)+0.5, float64(y)+0.5) {
				c.blend(x, y, col)
			}
		}
	}
}

// blend puts col (straight alpha) over the pixel (premultiplied): out = col·a + dst·(1 − a).
func (c *canvas) blend(x, y int, col color.NRGBA) {
	a := uint32(col.A)
	if a == 0 {
		return
	}
	d := c.RGBAAt(x, y)
	mix := func(s, d uint8) uint8 { return uint8((uint32(s)*a + uint32(d)*(255-a)) / 255) }
	c.SetRGBA(x, y, color.RGBA{R: mix(col.R, d.R), G: mix(col.G, d.G), B: mix(col.B, d.B), A: uint8(a + uint32(d.A)*(255-a)/255)})
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

// grayOut fades the picture to the offline state's gray: color down to a fifth, a little darker.
func grayOut(img *image.RGBA) {
	for i := 0; i+3 < len(img.Pix); i += 4 {
		r, g, b := float64(img.Pix[i]), float64(img.Pix[i+1]), float64(img.Pix[i+2])
		l := 0.299*r + 0.587*g + 0.114*b
		f := func(v float64) uint8 { return uint8(math.Max(0, math.Min(255, (l+(v-l)*0.2)*0.92))) }
		img.Pix[i], img.Pix[i+1], img.Pix[i+2] = f(r), f(g), f(b)
	}
}

func inRoundRect(x, y, x0, y0, x1, y1, rad float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Max(x0+rad, math.Min(x, x1-rad))
	cy := math.Max(y0+rad, math.Min(y, y1-rad))
	return math.Hypot(x-cx, y-cy) <= rad
}

func segmentDist(px, py float64, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	t := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		t = math.Max(0, math.Min(1, ((px-a[0])*dx+(py-a[1])*dy)/l))
	}
	return math.Hypot(px-(a[0]+t*dx), py-(a[1]+t*dy))
}
