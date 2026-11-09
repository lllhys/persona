#!/usr/bin/env python3
"""Check making an avatar from an image in the web app (#2404) against a mock hub.

Start a dedicated mock hub with one uploaded look and the web dev server pointing at it, then run:
    PORT=18793 MOCK_LOOK_UPLOADS=demo python3 scripts/mock/mock_hub.py
    PERSONA_HUB_URL=http://127.0.0.1:18793 npx vite --host 127.0.0.1 --port 5179 --strictPort   # in web/
    python3 scripts/test/web_avatar_upload_e2e.py http://127.0.0.1:5179 [--shots <directory>] [--lang en|zh-Hans|ja]

Settings › Persona › Avatar has an Upload card after the looks and a Delete action on the uploaded one (on hover and
keyboard focus with a mouse, always on a touch screen). The dialog takes a made-up picture, crops it (drag, wheel,
slider), shows a hub refusal as it is, then uploads: the body is a square PNG of at most 1024 px with the trimmed name,
and the new look is chosen. Deleting it asks first and brings the default back. A hub without `look_uploads` shows no
Upload card. Wide (1280) and phone width (390) in light and dark; screenshots contain only made-up data.
"""
import argparse
import base64
import json
import os
import struct
import tempfile
import zlib

from playwright.sync_api import expect, sync_playwright


def made_up_png(path, w=960, h=640):
    """A landscape picture (sky, sun, hills) without Pillow, so the crop has something to show."""
    rows = []
    for y in range(h):
        row = bytearray(b"\x00")
        for x in range(w):
            if (x - w * 0.62) ** 2 + (y - h * 0.38) ** 2 < (h * 0.16) ** 2:
                px = (255, 214, 92)
            elif y > h * 0.62 + 40 * ((x / w * 6.28) % 3.14 - 1.57) ** 2 / 2.5:
                px = (60 + y * 40 // h, 140 - y * 40 // h, 90)
            else:
                t = y / h
                px = (int(250 - 120 * t), int(150 + 60 * t), int(120 + 120 * t))
            row += bytes(px) + b"\xff"
        rows.append(bytes(row))
    chunk = lambda kind, data: struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)
    with open(path, "wb") as f:
        f.write(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 6, 0, 0, 0))
                + chunk(b"IDAT", zlib.compress(b"".join(rows), 6)) + chunk(b"IEND", b""))
    return path


def png_size(data):
    assert data[:8] == b"\x89PNG\r\n\x1a\n", "the upload is not a PNG"
    return struct.unpack(">II", data[16:24])


# The uploaded look MOCK_LOOK_UPLOADS=demo seeds (scripts/mock/mock_hub.py).
DEMO = "豆豆"

WORDS = {
    "en": {"upload": "Upload an image as an avatar", "dialog": "New avatar from an image", "name": "Name", "subtitle": "Description (optional)",
           "send": "Upload", "delete": "Delete {}", "confirm": "Delete", "mochi": "Mochi, the default avatar", "zoom": "Zoom", "avatar": "Avatar",
           "making": "Making the avatar…"},
    "zh-Hans": {"upload": "上传图片做形象", "dialog": "用图片做形象", "name": "名字", "subtitle": "描述（可选）",
                "send": "上传", "delete": "删除「{}」", "confirm": "删除", "mochi": "团子，默认形象", "zoom": "缩放", "avatar": "形象",
                "making": "正在生成形象…"},
    "ja": {"upload": "画像をアップロードしてアバターにする", "dialog": "画像からアバターを作成", "name": "名前", "subtitle": "説明（任意）",
           "send": "アップロード", "delete": "「{}」を削除", "confirm": "削除", "mochi": "だんご（デフォルトのアバター）", "zoom": "ズーム", "avatar": "アバター",
           "making": "アバターを作成しています…"},
}


def check(browser, base, width, theme, lang, image, shots):
    w = WORDS[lang]
    phone = width < 640
    context = browser.new_context(viewport={"width": width, "height": 900 if not phone else 844}, color_scheme=theme,
                                  has_touch=phone, is_mobile=phone, device_scale_factor=2)
    context.add_init_script(f"localStorage.setItem('persona.theme', {theme!r}); localStorage.setItem('persona.language', {lang!r})")
    page = context.new_page()
    page.set_default_timeout(15000)
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    name = f"Sunset {width} {theme}"
    try:
        page.goto(base + "/settings?tab=persona&token=mock")
        group = page.locator('section[data-settings-group="avatar"]')
        picker = group.get_by_role("radiogroup", name=w["avatar"])
        upload = picker.get_by_role("button", name=w["upload"], exact=True)
        expect(upload).to_be_visible()
        demo = picker.get_by_role("radio", name=DEMO, exact=True)
        expect(demo).to_be_visible()
        # Upload is the last card, in the same grid as the looks and as big as one
        assert picker.evaluate("g => g.lastElementChild.getAttribute('aria-label')") == w["upload"]
        card = demo.bounding_box()
        box = upload.bounding_box()
        assert abs(card["height"] - box["height"]) < 1 and abs(card["width"] - box["width"]) < 1, (card, box)
        trash = picker.get_by_role("button", name=w["delete"].format(DEMO), exact=True)
        reveal = trash.locator("xpath=..")
        if phone:
            # a touch screen has no hover: Delete is always there
            expect(reveal).to_have_css("opacity", "1")
        else:
            expect(reveal).to_have_css("opacity", "0")
            demo.focus()
            page.keyboard.press("Tab")
            expect(trash).to_be_focused()
            expect(reveal).to_have_css("opacity", "1")
            page.locator("body").evaluate("b => document.activeElement.blur()")
            demo.hover()
            expect(reveal).to_have_css("opacity", "1")
            if shots:
                group.screenshot(path=os.path.join(shots, f"picker-hover-{width}-{theme}.png"))
            page.mouse.move(0, 0)
            page.locator("body").evaluate("b => document.activeElement.blur()")
        if shots:
            page.wait_for_timeout(300)
            group.screenshot(path=os.path.join(shots, f"picker-{width}-{theme}.png"))

        upload.click()
        dialog = page.get_by_role("dialog", name=w["dialog"])
        expect(dialog).to_be_visible()
        send = dialog.get_by_role("button", name=w["send"], exact=True)
        expect(send).to_be_disabled()
        if shots:
            page.wait_for_timeout(350)
            (page if phone else dialog).screenshot(path=os.path.join(shots, f"upload-empty-{width}-{theme}.png"))
        dialog.locator('[data-testid="look-upload-file"]').set_input_files(image)
        crop = dialog.locator('[data-testid="look-upload-crop"]')
        expect(crop).to_be_visible()
        # the canvas shows the picture: its middle is not empty
        page.wait_for_function("() => { const c = document.querySelector('[data-testid=look-upload-crop] canvas'); if (!c || !c.width) return false; const d = c.getContext('2d').getImageData(c.width / 2, c.height / 2, 1, 1).data; return d[3] > 0; }")
        zoom = dialog.get_by_role("slider", name=w["zoom"])
        zoom.fill("1.6")
        assert abs(float(zoom.input_value()) - 1.6) < 0.01
        pixel = "() => Array.from(document.querySelector('[data-testid=look-upload-crop] canvas').getContext('2d').getImageData(10, 10, 1, 1).data).join()"
        before = page.evaluate(pixel)
        r = crop.bounding_box()
        page.mouse.move(r["x"] + r["width"] / 2, r["y"] + r["height"] / 2)
        page.mouse.down()
        page.mouse.move(r["x"] + r["width"] / 2 - 60, r["y"] + r["height"] / 2 + 40, steps=6)
        page.mouse.up()
        # the drag moved the picture under the circle
        page.wait_for_function(f"was => ({pixel})() !== was", arg=before)
        if not phone:
            page.mouse.move(r["x"] + r["width"] * 0.3, r["y"] + r["height"] * 0.3)
            page.mouse.wheel(0, -120)
            page.wait_for_function("() => Number(document.querySelector('[role=dialog] input[type=range]').value) > 1.6")
        # from the keyboard: the crop box takes the focus, + zooms in and the arrow keys move
        before = float(zoom.input_value())
        crop.focus()
        page.keyboard.press("+")
        page.keyboard.press("ArrowLeft")
        page.wait_for_function("z => Number(document.querySelector('[role=dialog] input[type=range]').value) > z", arg=before)
        dialog.get_by_label(w["name"], exact=True).fill(f"  {name}  ")
        dialog.get_by_label(w["subtitle"], exact=True).fill("Warm evening colors")
        expect(send).to_be_enabled()
        if shots:
            page.wait_for_timeout(250)
            (page if phone else dialog).screenshot(path=os.path.join(shots, f"upload-{width}-{theme}.png"))

        # A refusal from the hub is shown as it is, and the form stays for another try; the next try is held back a
        # moment so its progress can be seen.
        refusal = "There are already 50 uploaded avatars. Delete one first."
        state = {"refused": False, "body": None, "held": None}

        def route(r):
            if r.request.method != "POST":
                return r.continue_()
            if not state["refused"]:
                state["refused"] = True
                return r.fulfill(status=409, content_type="application/json", body=json.dumps({"error": refusal, "code": "too_many"}))
            state["body"] = json.loads(r.request.post_data)
            state["held"] = r

        page.route("**/api/v1/looks/uploads", route)
        send.click()
        expect(dialog.get_by_role("alert")).to_have_text(refusal)
        expect(send).to_be_enabled()
        if shots:
            (page if phone else dialog).screenshot(path=os.path.join(shots, f"upload-refused-{width}-{theme}.png"))
        send.click()
        expect(send).to_be_disabled()
        expect(dialog.get_by_text(w["making"], exact=True).or_(dialog.get_by_role("progressbar"))).to_be_visible()
        for _ in range(100):  # the route handler runs while Playwright waits
            if state["held"]:
                break
            page.wait_for_timeout(50)
        assert state["held"], "the upload never reached the hub"
        if shots:
            (page if phone else dialog).screenshot(path=os.path.join(shots, f"upload-sending-{width}-{theme}.png"))
        state["held"].continue_()
        expect(dialog).to_have_count(0)
        body = state["body"]
        assert body and body["name"] == name and body["subtitle"] == "Warm evening colors" and body["choose"] is True, {k: v for k, v in (body or {}).items() if k != "image"}
        side = png_size(base64.b64decode(body["image"]))
        assert side[0] == side[1] and 64 <= side[0] <= 1024, side
        made = picker.get_by_role("radio", name=name, exact=True)
        expect(made).to_have_attribute("aria-checked", "true")
        expect(picker.get_by_role("radio", checked=True)).to_have_count(1)
        expect(made).to_contain_text("Warm evening colors")
        if shots:
            page.wait_for_timeout(300)
            group.screenshot(path=os.path.join(shots, f"picker-uploaded-{width}-{theme}.png"))

        # Delete asks first, says it is the current one, then the default comes back.
        made_trash = picker.get_by_role("button", name=w["delete"].format(name), exact=True)
        if not phone:
            made.hover()
        made_trash.click()
        confirm = page.get_by_role("dialog")
        expect(confirm).to_be_visible()
        if shots:
            page.wait_for_timeout(350)
            (page if phone else confirm).screenshot(path=os.path.join(shots, f"delete-{width}-{theme}.png"))
        confirm.get_by_role("button", name=w["confirm"], exact=True).click()
        expect(page.get_by_role("dialog")).to_have_count(0)
        expect(made).to_have_count(0)
        expect(picker.get_by_role("radio", name=w["mochi"], exact=True)).to_have_attribute("aria-checked", "true")

        # The profile page has the same picker.
        page.goto(base + "/assistant?token=mock")
        profile = page.get_by_role("radiogroup", name=w["avatar"])
        expect(profile.get_by_role("button", name=w["upload"], exact=True)).to_be_visible()
        if shots:
            page.wait_for_timeout(300)
            profile.locator("xpath=../..").screenshot(path=os.path.join(shots, f"profile-{width}-{theme}.png"))
        assert not errors, errors
        print(f"PASS avatar upload {width}px {theme}", flush=True)
    except Exception:
        print(f"FAIL avatar upload {width}px {theme}; path={page.url.removeprefix(base)}; page errors={errors}", flush=True)
        if shots:
            page.screenshot(path=os.path.join(shots, f"failure-{width}-{theme}.png"))
        raise
    finally:
        context.close()


def check_old_hub(browser, base, lang):
    """A hub without look_uploads: no Upload card and no Delete, the picker as before."""
    w = WORDS[lang]
    context = browser.new_context(viewport={"width": 1280, "height": 900})
    context.add_init_script(f"localStorage.setItem('persona.language', {lang!r})")

    def socket(route):
        server = route.connect_to_server()

        def message(raw):
            data = json.loads(raw)
            if data.get("type") == "hello":
                data["features"] = [f for f in data.get("features", []) if f != "look_uploads"]
            route.send(json.dumps(data))

        server.on_message(message)

    context.route_web_socket("**/api/v1/ws?*", socket)
    try:
        page = context.new_page()
        page.goto(base + "/settings?tab=persona&token=mock")
        picker = page.locator('section[data-settings-group="avatar"]').get_by_role("radiogroup", name=w["avatar"])
        expect(picker.get_by_role("radio", name=w["mochi"], exact=True)).to_be_visible()
        expect(picker.get_by_role("radio", name=DEMO, exact=True)).to_be_visible()
        expect(picker.get_by_role("button", name=w["upload"], exact=True)).to_have_count(0)
        expect(picker.get_by_role("button", name=w["delete"].format(DEMO), exact=True)).to_have_count(0)
        print("PASS avatar upload hidden without look_uploads", flush=True)
    finally:
        context.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("base")
    parser.add_argument("--shots")
    parser.add_argument("--lang", default="en", choices=sorted(WORDS))
    parser.add_argument("--channel", default="chrome", help="Playwright browser channel (default: the installed Chrome)")
    args = parser.parse_args()
    if args.shots:
        os.makedirs(args.shots, exist_ok=True)
    base = args.base.rstrip("/")
    with tempfile.TemporaryDirectory(prefix="avatar-upload-") as tmp, sync_playwright() as playwright:
        image = made_up_png(os.path.join(tmp, "sunset.png"))
        browser = playwright.chromium.launch(channel=args.channel)
        try:
            check_old_hub(browser, base, args.lang)
            for width in (1280, 390):
                for theme in ("light", "dark"):
                    check(browser, base, width, theme, args.lang, image, args.shots)
        finally:
            browser.close()
    print("PASS web avatar upload e2e")


if __name__ == "__main__":
    main()
