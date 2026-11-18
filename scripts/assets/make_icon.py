#!/usr/bin/env python3
"""App icon: "Paper", the hand-drawn helper waving on warm paper (#709, picked by the user in 2026-11). The art was
generated and chosen, not drawn here: the iPhone set's icon.png (light) and icon-dark.png (dark) are it, 1024, RGB,
no alpha. This derives the rest from them: the tinted look, the watch icon (the light one; the system masks it round),
the Mac sizes, the desktop app's icons (write_desktop) and a temporary icon preview:
  python3 scripts/assets/make_icon.py
  python3 scripts/assets/make_icon.py mac      # only the Mac sizes
  python3 scripts/assets/make_icon.py vision   # only Apple Vision Pro's icon
  python3 scripts/assets/make_icon.py desktop  # only the desktop app's icons

The Mac icon is the same picture on macOS's icon grid: the system does not mask a Mac icon, so it carries its own
rounded square (824 of 1024, centered) and shadow, at every size the Mac App Store asks for (16 to 512, 1x and 2x).
Apple Vision Pro takes no app icon set: its icon is a layered image stack (AppIcon-Vision.solidimagestack, the
PersonaVision target's ASSETCATALOG_COMPILER_APPICON_NAME), whose back layer must be opaque. The art is one flat
picture, so make_icon.py splits it: the paper (fitted from the border) is the back layer, the drawing on it the
front one, the middle layer empty; the system masks it round.
Replacing the icon is overwriting the two PNGs and rerunning this (and scripts/assets/make_alt_icons.py for the
settings preview).
"""
import json
import os
import sys
import tempfile

from PIL import Image, ImageChops, ImageDraw, ImageFilter, ImageOps

S = 1024
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
IOS = os.path.join(ROOT, "apple/Persona/Resources/Assets.xcassets/AppIcon.appiconset")
WATCH = os.path.join(ROOT, "apple/PersonaWatch/Assets.xcassets/AppIcon.appiconset")
VISION = os.path.join(ROOT, "apple/Persona/Resources/Assets.xcassets/AppIcon-Vision.solidimagestack")
MAC_SIZES = (16, 32, 128, 256, 512)


def tinted_from(dark):
    """The tinted look from the dark one: greyscale, levels stretched so the whole ground is black (the system colours
    it by brightness): the brightest pixel of a band along the edges, where only the ground is, becomes black."""
    g = ImageOps.grayscale(dark.convert("RGB"))
    band = S // 25
    edges = [g.crop(box) for box in ((0, 0, S, band), (0, S - band, S, S), (0, 0, band, S), (S - band, 0, S, S))]
    lo = min(200, max(max(e.getdata()) for e in edges) + 4)
    return g.point(lambda v: max(0, min(255, (v - lo) * 255 // (255 - lo)))).convert("RGB")


def mac_icon(picture):
    """macOS's grid at 1024: an 824 rounded square (corner 185) 100 in from the left and right, lifted a little
    above center (the shadow sits under it), with the picture filling it."""
    body, radius, top = 824, 185, 88
    big = 4  # drawn at 4x for smooth corners, then downsampled
    canvas = Image.new("RGBA", (S * big, S * big), (0, 0, 0, 0))
    mask = Image.new("L", (body * big, body * big), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, body * big - 1, body * big - 1], radius=radius * big, fill=255)
    left = (S - body) // 2
    shadow = Image.new("RGBA", canvas.size, (0, 0, 0, 0))
    shadow.paste((0, 0, 0, 90), (left * big, (top + 12) * big), mask)
    canvas = Image.alpha_composite(canvas, shadow.filter(ImageFilter.GaussianBlur(14 * big)))
    art = picture.convert("RGBA").resize((body * big, body * big), Image.LANCZOS)
    canvas.paste(art, (left * big, top * big), mask)
    return canvas.resize((S, S), Image.LANCZOS)


def write_mac(picture):
    """The Mac sizes into the iPhone icon's set (the Mac app builds the same asset catalog; iOS ignores idiom mac)."""
    icon = mac_icon(picture)
    path = os.path.join(IOS, "Contents.json")
    with open(path) as f:
        contents = json.load(f)
    contents["images"] = [i for i in contents["images"] if i.get("idiom") != "mac"]
    for size in MAC_SIZES:
        for scale in (1, 2):
            name = f"mac-{size}{'@2x' if scale == 2 else ''}.png"
            icon.resize((size * scale, size * scale), Image.LANCZOS).save(os.path.join(IOS, name))
            contents["images"].append({"filename": name, "idiom": "mac", "scale": f"{scale}x", "size": f"{size}x{size}"})
    with open(path, "w") as f:
        json.dump(contents, f, indent=2)
        f.write("\n")


def write_set(folder, images, contents):
    os.makedirs(folder, exist_ok=True)
    for name, im in images.items():
        im.save(os.path.join(folder, name))
    with open(os.path.join(folder, "Contents.json"), "w") as f:
        json.dump(contents, f, indent=2)


def vision_layers(picture):
    """The flat art split into Apple Vision Pro's two layers: the paper alone (a smooth fit of the border band, opaque)
    and the drawing on it (where the art differs from the paper, with a soft edge), so the drawing floats over the
    paper and composing the two gives the art back."""
    import numpy as np
    art = np.asarray(picture.convert("RGB"), dtype=np.float64)
    h, w, _ = art.shape
    ys, xs = np.mgrid[0:h, 0:w] / max(h, w)
    band = np.zeros((h, w), bool)
    b = 48
    band[:b, :] = band[-b:, :] = band[:, :b] = band[:, -b:] = True
    terms = lambda x, y: np.stack([np.ones_like(x), x, y, x * x, x * y, y * y], -1)
    a = terms(xs[band], ys[band])
    paper = np.empty_like(art)
    for c in range(3):
        coef, *_ = np.linalg.lstsq(a, art[..., c][band], rcond=None)
        paper[..., c] = terms(xs, ys) @ coef
    diff = np.abs(art - paper).max(-1)
    alpha = np.clip((diff - 6) / 18, 0, 1)
    back = Image.fromarray(np.clip(paper, 0, 255).astype(np.uint8), "RGB")
    mask = Image.fromarray((alpha * 255).astype(np.uint8), "L").filter(ImageFilter.GaussianBlur(1.5))
    front = picture.convert("RGBA")
    front.putalpha(mask)
    return back, front


def write_vision(picture):
    """Apple Vision Pro's layered icon: the drawing in front, the paper behind (opaque), the middle layer empty."""
    info = {"author": "xcode", "version": 1}
    back, front = vision_layers(picture)
    pictures = {"Front": ("front.png", front), "Back": ("back.png", back)}
    layers = ("Front", "Middle", "Back")
    os.makedirs(VISION, exist_ok=True)
    with open(os.path.join(VISION, "Contents.json"), "w") as f:
        json.dump({"info": info, "layers": [{"filename": f"{l}.solidimagestacklayer"} for l in layers]}, f, indent=2)
    for layer in layers:
        folder = os.path.join(VISION, f"{layer}.solidimagestacklayer")
        os.makedirs(folder, exist_ok=True)
        with open(os.path.join(folder, "Contents.json"), "w") as f:
            json.dump({"info": info}, f, indent=2)
        image = {"idiom": "vision", "scale": "2x"}
        images = {}
        if layer in pictures:
            name, im = pictures[layer]
            image["filename"] = name
            images[name] = im
        write_set(os.path.join(folder, "Content.imageset"), images, {"images": [image], "info": info})


def tray_marks(picture):
    """The helper alone, for the desktop app's tray, from the drawing's layer (vision_layers): its outline, eyes, cheek
    and white body, without the floor shadow under it (mid-tone, neither white nor ink), on a square around it. Gives
    (crop box, colour drawing, menu bar mark): the colour one is the drawing with its body filled in; the menu bar
    one is a template (black, the system colours it): the ink strokes thickened to read at 16-18 points, the waving
    lines thinner so they stay apart."""
    import numpy as np
    _, front = vision_layers(picture)
    a = np.asarray(front).astype(np.float64)
    alpha = a[..., 3] / 255
    lum = a[..., :3].mean(-1)
    cheek = (a[..., 0] > 200) & (a[..., 1] < 150)
    ink = (alpha > 0.5) & ((lum > 225) | (lum < 120) | cheek)
    ys, xs = np.nonzero(ink)
    side = max(ys.max() - ys.min(), xs.max() - xs.min()) + 20
    cy, cx = (ys.min() + ys.max()) // 2, (xs.min() + xs.max()) // 2
    box = (cx - side // 2, cy - side // 2, cx + side // 2, cy + side // 2)
    grey = lambda m: Image.fromarray((m * 255).astype(np.uint8), "L")
    filled = grey(ink).filter(ImageFilter.MaxFilter(5)).filter(ImageFilter.MinFilter(5))
    ImageDraw.floodfill(filled, (0, 0), 128)  # the outside; everything else is the helper
    body = np.asarray(filled) != 128
    dark = (lum < 120) & (alpha > 0.5)
    colour = front.copy()
    colour.putalpha(ImageChops.lighter(grey(body).filter(ImageFilter.GaussianBlur(1.5)), grey(dark & ~body)))
    strokes = ImageChops.lighter(grey(dark & body).filter(ImageFilter.MaxFilter(25)),
                                 grey(dark & ~body).filter(ImageFilter.MaxFilter(13)))
    mark = Image.new("RGBA", picture.size, (0, 0, 0, 0))
    mark.putalpha(strokes.filter(ImageFilter.GaussianBlur(3)))
    return colour.crop(box), mark.crop(box)


def write_desktop(picture):
    """The desktop app's icons (desktop/, docs/desktop/desktop.md "Icons"): the app icon electron-builder packs (macOS:
    the Mac icon above, from which it makes the .icns; Windows: the art in a rounded square, all the sizes an .ico
    holds), and the tray's: macOS's menu bar template at 1x and 2x (Electron picks @2x and takes a name ending in
    Template as a template image), Windows' colour helper in an .ico of the notification area's sizes (16 at 100 % to
    32 at 200 %), and a colour PNG for development runs elsewhere."""
    build = os.path.join(ROOT, "desktop/build")
    assets = os.path.join(ROOT, "desktop/src/assets")
    os.makedirs(build, exist_ok=True)
    os.makedirs(assets, exist_ok=True)
    mac_icon(picture).save(os.path.join(build, "icon.png"))
    big = 4
    mask = Image.new("L", (S * big, S * big), 0)
    inset = 32 * big
    ImageDraw.Draw(mask).rounded_rectangle([inset, inset, S * big - inset - 1, S * big - inset - 1], radius=180 * big, fill=255)
    win = Image.new("RGBA", (S * big, S * big), (0, 0, 0, 0))
    win.paste(picture.convert("RGBA").resize((S * big, S * big), Image.LANCZOS), (0, 0), mask)
    win = win.resize((256, 256), Image.LANCZOS)
    win.save(os.path.join(build, "icon.ico"), sizes=[(s, s) for s in (16, 24, 32, 48, 64, 128, 256)])
    colour, mark = tray_marks(picture)
    mark.resize((18, 18), Image.LANCZOS).save(os.path.join(assets, "trayTemplate.png"))
    mark.resize((36, 36), Image.LANCZOS).save(os.path.join(assets, "trayTemplate@2x.png"))
    tray = colour.resize((256, 256), Image.LANCZOS)
    tray.save(os.path.join(assets, "tray.ico"), sizes=[(s, s) for s in (16, 20, 24, 32, 40, 48)])
    colour.resize((32, 32), Image.LANCZOS).save(os.path.join(assets, "tray.png"))


def main():
    light = Image.open(os.path.join(IOS, "icon.png")).convert("RGB")
    dark = Image.open(os.path.join(IOS, "icon-dark.png")).convert("RGB")
    tinted = tinted_from(dark)
    write_set(IOS, {"icon.png": light, "icon-dark.png": dark, "icon-tinted.png": tinted}, {
        "images": [
            {"filename": "icon.png", "idiom": "universal", "platform": "ios", "size": "1024x1024"},
            {"appearances": [{"appearance": "luminosity", "value": "dark"}], "filename": "icon-dark.png",
             "idiom": "universal", "platform": "ios", "size": "1024x1024"},
            {"appearances": [{"appearance": "luminosity", "value": "tinted"}], "filename": "icon-tinted.png",
             "idiom": "universal", "platform": "ios", "size": "1024x1024"},
        ],
        "info": {"author": "xcode", "version": 1},
    })
    write_mac(light)
    write_vision(light)
    write_desktop(light)
    write_set(WATCH, {"icon.png": light}, {
        "images": [{"filename": "icon.png", "idiom": "universal", "platform": "watchos", "size": "1024x1024"}],
        "info": {"author": "xcode", "version": 1},
    })
    preview = Image.new("RGB", (S * 3 + 80, S + 40), (245, 243, 238))
    mask = Image.new("L", (S, S), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, S, S], radius=int(S * 0.225), fill=255)
    for i, im in enumerate((light, dark, tinted)):
        preview.paste(im, (20 + i * (S + 20), 20), mask)
    preview.resize((preview.width // 2, preview.height // 2), Image.LANCZOS).save(os.path.join(tempfile.gettempdir(), "persona-icon-preview.png"))
    print("icons written")


if __name__ == "__main__":
    if sys.argv[1:] == ["mac"]:
        write_mac(Image.open(os.path.join(IOS, "icon.png")))
        print("mac icons written")
    elif sys.argv[1:] == ["vision"]:
        write_vision(Image.open(os.path.join(IOS, "icon.png")))
        print("vision icon written")
    elif sys.argv[1:] == ["desktop"]:
        write_desktop(Image.open(os.path.join(IOS, "icon.png")).convert("RGB"))
        print("desktop icons written")
    else:
        main()
